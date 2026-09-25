package video_api

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/contextwindow"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// HandleGenerationRetry queues guided context generation or an audio range repair.
func HandleGenerationRetry(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return err
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		video, err := common.RequireVideo(c, dbc.Queries(c.Request().Context()), id, plugin.ActionVideoWrite)
		if err != nil {
			return err
		}
		var in struct {
			Mode         string `json:"retryMode"`
			Instructions string `json:"retryInstructions"`
			Start        string `json:"repairStart"`
			End          string `json:"repairEnd"`
		}
		notice := func(message string) error {
			return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.GenerationRetryNotice(message))
		}
		if err := datastar.ReadSignals(c.Request(), &in); err != nil {
			return notice("Unable to read retry options.")
		}
		if len(in.Instructions) > 4000 {
			return notice("Please keep guidance under 4000 bytes.")
		}
		if in.Mode != "context" && in.Mode != "transcript" {
			return notice("Choose context or transcript repair.")
		}
		ctx := c.Request().Context()
		q, tx, err := dbc.NewWithTX(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err := q.LockVideoForContext(ctx, id); err != nil {
			return err
		}
		kind := "context_windows"
		if in.Mode == "transcript" {
			kind = "transcribe"
		}
		jobs, err := q.ListMLJobsForVideo(ctx, id)
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.Kind == kind && (job.Status == "queued" || job.Status == "processing") {
				return notice("This video already has queued or running work of this kind. Check Processing status before retrying.")
			}
		}
		if in.Mode == "context" {
			_, err = contextwindow.EnqueueRetry(ctx, q, id, in.Instructions)
		} else {
			start, e1 := parseRepairTime(in.Start)
			end, e2 := parseRepairTime(in.End)
			if e1 != nil || e2 != nil || end <= start || end-start > 3600 || video.DurationSeconds == nil || end > float64(*video.DurationSeconds) {
				return notice("Enter a valid range within the video, up to one hour, using seconds or HH:MM:SS.")
			}
			if video.Media == "metadata" {
				return notice("Archive the video before repairing its transcript.")
			}
			hash, hashErr := q.GetTranscriptFingerprint(ctx, id)
			if hashErr != nil {
				return hashErr
			}
			_, err = q.EnqueueGenerationRetry(ctx, &db.EnqueueGenerationRetryParams{VideoID: id, Kind: kind, TranscriptHash: hash, PromptVersion: "repair-v1:" + uuid.NewString(), RangeStart: &start, RangeEnd: &end, RetryInstructions: strings.TrimSpace(in.Instructions), RepairTranscript: true})
		}
		if err != nil {
			return notice("Could not queue retry: " + err.Error())
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		if in.Mode == "transcript" {
			return notice("Audio repair queued. The previous transcript is backed up before replacement; context will regenerate with your guidance after repair succeeds.")
		}
		return notice("Fresh context generation queued with your guidance. Previous results remain available until it succeeds.")
	}
}

func parseRepairTime(raw string) (float64, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("invalid time")
	}
	var total float64
	for i, part := range parts {
		n, err := strconv.ParseFloat(part, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || (i > 0 && n >= 60) {
			return 0, fmt.Errorf("invalid time")
		}
		total = total*60 + n
	}
	return total, nil
}
