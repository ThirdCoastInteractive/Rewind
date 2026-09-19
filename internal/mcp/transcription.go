package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	xlanguage "golang.org/x/text/language"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/jsnum"
	"thirdcoast.systems/rewind/pkg/plugin"
)

type transcribeArgs struct {
	VideoID string   `json:"video_id" jsonschema:"Archived video UUID"`
	Start   *jsnum.F `json:"start,omitempty" jsonschema:"Absolute start seconds; supply both start/end for a range, omit both for the full video"`
	End     *jsnum.F `json:"end,omitempty" jsonschema:"Absolute end seconds; range length at most 1800 seconds"`
}

func registerTranscriptionTools(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "enqueue_transcribe", Description: "Queue full-video or bounded-range speech transcription. Returns a durable job ID; identical requests reuse the job. Partial results are stored with absolute timestamps and explicit coverage. Requires mcp:write; never downloads models."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *transcribeArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(a.VideoID)
		if err != nil {
			return nil, nil, err
		}
		if (a.Start == nil) != (a.End == nil) {
			return nil, nil, fmt.Errorf("supply both start and end, or neither")
		}
		key := "full"
		var start, end *float64
		if a.Start != nil {
			s, e := float64(*a.Start), float64(*a.End)
			if math.IsNaN(s) || math.IsNaN(e) || math.IsInf(s, 0) || math.IsInf(e, 0) || s < 0 || e <= s || e-s > 1800 {
				return nil, nil, fmt.Errorf("range must be finite, positive and at most 1800 seconds")
			}
			start, end = &s, &e
			key = fmt.Sprintf("range:%g:%g", s, e)
		}
		video, err := dbc.Queries(ctx).GetVideoByID(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if video.Media == "metadata" {
			return nil, nil, fmt.Errorf("media is not archived; download it before transcription")
		}
		if end != nil && video.DurationSeconds != nil && *video.DurationSeconds > 0 && *end > float64(*video.DurationSeconds) {
			return nil, nil, fmt.Errorf("range exceeds video duration")
		}
		jobID, err := plugin.EnqueueID(ctx, plugin.Job{
			VideoID: a.VideoID, Kind: plugin.KindTranscribe, Priority: 10,
			RequestKey: key, RangeStart: start, RangeEnd: end, PromptVersion: "asr-v1",
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"job_id": jobID, "status": "queued", "video_id": a.VideoID, "start": start, "end": end, "next_tool": "get_transcription_status"})
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "get_transcription_status", Description: "Read a transcription job's state, failure reason and available transcript coverage. On success use get_transcript; waiting_model requires runtime/model repair."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *retryMLJobArgs) (*mcpsdk.CallToolResult, any, error) {
		if tokenFrom(ctx) == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		id, err := parseUUID(a.JobID)
		if err != nil {
			return nil, nil, err
		}
		job, err := dbc.Queries(ctx).GetMLJob(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				if indexed, indexErr := indexJobJSON(ctx, dbc.Queries(ctx), id); indexErr == nil {
					return nil, nil, fmt.Errorf("job is %s ingest, not transcription; use get_index_status", indexed["kind"])
				}
			}
			return nil, nil, err
		}
		if job.Kind != "transcribe" {
			return nil, nil, fmt.Errorf("not a transcription job")
		}
		coverage, err := dbc.Queries(ctx).ListTranscriptCoverage(ctx, job.VideoID)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"job_id": a.JobID, "video_id": uuidString(job.VideoID), "status": job.Status, "last_error": job.LastError, "start": job.RangeStart, "end": job.RangeEnd, "transcripts": coverageInfo(coverage), "next_tool": "get_transcript"})
	})
}

func coverageInfo(rows []*db.ListTranscriptCoverageRow) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{"language": xlanguage.Tag(r.Lang).String(), "complete": len(r.Coverage) == 0, "ranges": json.RawMessage(r.Coverage)})
	}
	return out
}
