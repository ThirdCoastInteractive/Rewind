package video_api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/contextwindow"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/topics"
)

type contextWindowInput struct {
	Start           float64         `json:"start"`
	End             float64         `json:"end"`
	Title           string          `json:"title"`
	Summary         string          `json:"summary"`
	Topics          []string        `json:"topics"`
	Entities        []string        `json:"entities"`
	SourceQuery     string          `json:"source_query"`
	Evidence        json.RawMessage `json:"evidence"`
	BoundaryQuality string          `json:"boundary_quality"`
}

// HandleContextWindowsList returns Context Windows intersecting an optional range.
func HandleContextWindowsList(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		videoID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		rows, err := dbc.Queries(c.Request().Context()).ListContextWindowsForVideo(c.Request().Context(), &db.ListContextWindowsForVideoParams{VideoID: videoID, StartTs: optionalFloat(c.QueryParam("start")), EndTs: optionalFloat(c.QueryParam("end"))})
		if err != nil {
			return err
		}
		if c.QueryParam("render") == "1" {
			rows = filterWatchContextWindows(rows, c.QueryParam("q"))
			chips, neighbors := watchTopicExtras(c.Request().Context(), dbc, videoID, rows)
			return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.WatchContextWindows(videoID.String(), rows, chips, neighbors))
		}
		return c.JSON(200, rows)
	}
}

// HandleContextWindowCreate creates a user-authored Context Window.
func HandleContextWindowCreate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		videoID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		var in contextWindowInput
		if err := c.Bind(&in); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid context window")
		}
		video, err := dbc.Queries(c.Request().Context()).GetVideoByID(c.Request().Context(), videoID)
		if err != nil {
			return err
		}
		if err := contextwindow.Validate(in.Start, in.End, in.Title, video.DurationSeconds); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		quality := normalizeBoundaryQuality(in.BoundaryQuality)
		evidence := in.Evidence
		if len(evidence) == 0 {
			evidence = json.RawMessage(`[]`)
		}
		row, err := dbc.Queries(c.Request().Context()).CreateContextWindow(c.Request().Context(), &db.CreateContextWindowParams{
			VideoID: videoID, StartTs: in.Start, EndTs: in.End, Title: strings.TrimSpace(in.Title), Summary: strings.TrimSpace(in.Summary),
			Topics: in.Topics, Entities: in.Entities, Origin: "user", SourceQuery: strings.TrimSpace(in.SourceQuery),
			TranscriptCueEvidence: evidence, BoundaryQuality: quality, CreatedBy: userID,
		})
		if err != nil {
			return err
		}
		ts := topics.New(dbc)
		_ = ts.SeedWiki(c.Request().Context())
		_ = ts.BindWindow(c.Request().Context(), row.ID, row.Title, row.Topics, row.Entities)
		return c.JSON(http.StatusCreated, row)
	}
}

// HandleContextWindowUpdate updates a Context Window while preserving omitted metadata.
func HandleContextWindowUpdate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return echo.NewHTTPError(401)
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		var in contextwindow.Patch
		if err := c.Bind(&in); err != nil {
			return echo.NewHTTPError(400, "invalid context window")
		}
		row, err := contextwindow.Edit(c.Request().Context(), dbc, id, in)
		if err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
		if row != nil {
			ts := topics.New(dbc)
			_ = ts.SeedWiki(c.Request().Context())
			_ = ts.BindWindow(c.Request().Context(), row.ID, row.Title, row.Topics, row.Entities)
		}
		return c.JSON(200, row)
	}
}

// HandleContextWindowDelete deletes one Context Window without touching its video.
func HandleContextWindowDelete(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		if err := dbc.Queries(c.Request().Context()).DeleteContextWindow(c.Request().Context(), id); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func optionalFloat(raw string) *float64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var v float64
	if _, err := fmt.Sscan(raw, &v); err != nil {
		return nil
	}
	return &v
}

// HandleGenerateContextWindows queues context generation for the current transcript.
func HandleGenerateContextWindows(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		videoID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		if err := contextwindow.Enqueue(c.Request().Context(), dbc, videoID); err != nil {
			if c.QueryParam("render") == "1" {
				return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.VideoProcessingNotice(err.Error()))
			}
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if c.QueryParam("render") == "1" {
			if err := datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.VideoProcessingNotice("Context requested. Status is under Generated data; the processing queue is on Jobs → Video processing.")); err != nil {
				return err
			}
			return HandleJobs(sm, dbc)(c)
		}
		return c.JSON(http.StatusAccepted, map[string]string{"status": "queued"})
	}
}

// HandleRetryMLJob requeues one ML job for operator retry.
func HandleRetryMLJob(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		n, err := dbc.Queries(c.Request().Context()).RetryMLJob(c.Request().Context(), id)
		if err != nil {
			return err
		}
		if n == 0 {
			return echo.NewHTTPError(http.StatusConflict, "job is not retryable")
		}
		if c.QueryParam("render") == "jobs" {
			return datastar.NewSSE(c.Response(), c.Request()).PatchSignals([]byte(`{"jobsNotice":"Processing job queued for retry."}`))
		}
		if c.QueryParam("render") == "1" {
			return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.VideoProcessingNotice("Job queued for retry. Runtime cooldowns still apply; status refreshes within 5 seconds."))
		}
		if strings.Contains(c.Request().Header.Get("Content-Type"), "application/json") {
			return c.JSON(http.StatusAccepted, map[string]string{"status": "queued"})
		}
		return c.Redirect(http.StatusSeeOther, "/jobs")
	}
}

// HandleMLRuntimeHealth returns per-kind inference cooldown and last error.
func HandleMLRuntimeHealth(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		rows, err := dbc.Queries(c.Request().Context()).ListMLRuntimeHealth(c.Request().Context())
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, rows)
	}
}

// HandleMLJobs lists recent ML jobs for the operator coverage surface.
func HandleMLJobs(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		ctx := c.Request().Context()
		jobs, err := dbc.Queries(ctx).ListRecentMLJobs(ctx)
		if err != nil {
			return err
		}
		health, err := dbc.Queries(ctx).ListMLRuntimeHealth(ctx)
		if err != nil {
			return err
		}
		counts, err := dbc.Queries(ctx).CountMLJobs(ctx)
		if err != nil {
			return err
		}
		if datastarRender(c) {
			return patchMLJobs(c, jobs, health, counts, "")
		}
		return c.JSON(http.StatusOK, jobs)
	}
}

// HandleClearMLQueue cancels queued, waiting, and in-flight ML jobs.
func HandleClearMLQueue(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		kind := strings.TrimSpace(c.QueryParam("kind"))
		n, err := dbc.Queries(c.Request().Context()).CancelClaimableMLJobs(c.Request().Context(), &db.CancelClaimableMLJobsParams{Kind: kind, MinPriority: 0})
		if err != nil {
			return err
		}
		notice := fmt.Sprintf("Cancelled %d jobs. Agents can enqueue what they need.", n)
		if !datastarRender(c) {
			return c.JSON(http.StatusOK, map[string]any{"status": "cancelled", "cancelled": n, "notice": notice})
		}
		ctx := c.Request().Context()
		jobs, err := dbc.Queries(ctx).ListRecentMLJobs(ctx)
		if err != nil {
			return err
		}
		health, err := dbc.Queries(ctx).ListMLRuntimeHealth(ctx)
		if err != nil {
			return err
		}
		counts, err := dbc.Queries(ctx).CountMLJobs(ctx)
		if err != nil {
			return err
		}
		return patchMLJobs(c, jobs, health, counts, notice)
	}
}

func datastarRender(c echo.Context) bool {
	return c.QueryParam("render") == "1" || strings.Contains(c.Request().Header.Get("Accept"), "text/event-stream")
}

func patchMLJobs(c echo.Context, jobs []*db.ListRecentMLJobsRow, health []*db.MlRuntimeHealth, counts []*db.CountMLJobsRow, notice string) error {
	sse := datastar.NewSSE(c.Response(), c.Request())
	if err := sse.PatchElementTempl(templates.MLJobsList(jobs, health, counts, notice), datastar.WithSelectorID("ml-jobs-list")); err != nil {
		return err
	}
	return sse.PatchSignals([]byte(`{"clearingMl":false}`))
}

// HandleSetMLJobPriority reorders one claimable ML job.
func HandleSetMLJobPriority(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			Priority int32 `json:"priority"`
		}
		if err := c.Bind(&in); err != nil || in.Priority < 1 || in.Priority > 1000 {
			return echo.NewHTTPError(http.StatusBadRequest, "priority must be between 1 and 1000")
		}
		n, err := dbc.Queries(c.Request().Context()).SetMLJobPriority(c.Request().Context(), &db.SetMLJobPriorityParams{ID: id, Priority: in.Priority})
		if err != nil {
			return err
		}
		if n == 0 {
			return echo.NewHTTPError(http.StatusConflict, "job is not reorderable")
		}
		return c.JSON(http.StatusOK, map[string]any{"status": "queued", "priority": in.Priority})
	}
}

func normalizeBoundaryQuality(value string) string {
	switch value {
	case "waveform", "manual":
		return value
	default:
		return "cue"
	}
}

// filterWatchContextWindows matches every search word within the current video's metadata.
func filterWatchContextWindows(rows []*db.ListContextWindowsForVideoRow, query string) []*db.ListContextWindowsForVideoRow {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return rows
	}
	matchesText := func(row *db.ListContextWindowsForVideoRow) bool {
		if row == nil {
			return false
		}
		text := strings.ToLower(strings.Join([]string{row.Title, row.Summary, row.Hook, strings.Join(row.Topics, " "), strings.Join(row.Entities, " ")}, " "))
		for _, term := range terms {
			if !strings.Contains(text, term) {
				return false
			}
		}
		return true
	}
	keep := make([]bool, len(rows))
	for i, row := range rows {
		if !matchesText(row) {
			continue
		}
		keep[i] = true
		if row.Kind == "short" && row.ParentID.Valid {
			for j, parent := range rows {
				if parent != nil && parent.ID.Valid && parent.ID.Bytes == row.ParentID.Bytes {
					keep[j] = true
				}
			}
			continue
		}
		if !row.ID.Valid {
			continue
		}
		for j, sh := range rows {
			if sh != nil && sh.Kind == "short" && sh.ParentID.Valid && sh.ParentID.Bytes == row.ID.Bytes {
				keep[j] = true
			}
		}
	}
	matches := make([]*db.ListContextWindowsForVideoRow, 0, len(rows))
	for i, row := range rows {
		if row != nil && keep[i] {
			matches = append(matches, row)
		}
	}
	return matches
}

func watchTopicExtras(ctx context.Context, dbc *db.DatabaseConnection, videoID pgtype.UUID, rows []*db.ListContextWindowsForVideoRow) (map[string][]templates.TopicChip, []templates.TopicNeighbor) {
	chips := map[string][]templates.TopicChip{}
	if dbc == nil {
		return chips, nil
	}
	q := dbc.Queries(ctx)
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, r := range rows {
		if r != nil && r.Kind != "short" {
			ids = append(ids, r.ID)
		}
	}
	if len(ids) > 0 {
		binds, err := q.ListWindowTopicBinds(ctx, ids)
		if err == nil {
			for _, b := range binds {
				if b == nil {
					continue
				}
				key := b.WindowID.String()
				chips[key] = append(chips[key], templates.TopicChip{Slug: b.Slug, Title: b.Title})
			}
		}
	}
	var neighbors []templates.TopicNeighbor
	ns, err := q.ListTopicNeighborsForVideo(ctx, &db.ListTopicNeighborsForVideoParams{VideoID: videoID, PageLimit: 8})
	if err == nil {
		seen := map[string]bool{}
		for _, n := range ns {
			if n == nil {
				continue
			}
			vid := n.VideoID.String()
			if seen[vid] {
				continue
			}
			seen[vid] = true
			neighbors = append(neighbors, templates.TopicNeighbor{
				VideoID: vid, Title: n.Title, VideoTitle: n.VideoTitle, Uploader: n.Uploader,
				Start: n.StartTs, TopicTitle: n.TopicTitle,
				WebPath: fmt.Sprintf("/videos/%s?t=%.3f", vid, n.StartTs),
			})
		}
	}
	return chips, neighbors
}
