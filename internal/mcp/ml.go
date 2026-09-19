package mcp

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/contextwindow"
	"thirdcoast.systems/rewind/internal/db"
)

type enqueueContextArgs struct {
	VideoID string `json:"video_id" jsonschema:"Archived video UUID"`
}

type retryMLJobArgs struct {
	JobID string `json:"job_id" jsonschema:"ML job UUID"`
}

type listMLJobsArgs struct {
	Kind   string `json:"kind,omitempty" jsonschema:"Optional kind: transcribe, context_windows, visual_index, refine_boundaries"`
	Status string `json:"status,omitempty" jsonschema:"Optional status: queued, processing, cancelled, waiting_model, succeeded, failed"`
	Limit  int32  `json:"limit,omitempty" jsonschema:"Max rows, default 50, max 200"`
}

type setMLJobPriorityArgs struct {
	JobID    string `json:"job_id" jsonschema:"ML job UUID"`
	Priority int32  `json:"priority" jsonschema:"Lower runs first. Explicit/agent context is 100; demand backfill is 120-160; archive dump was 200."`
}

type clearMLQueueArgs struct {
	Kind           string `json:"kind,omitempty" jsonschema:"Optional kind to pause. Empty = every kind."`
	BackgroundOnly *bool  `json:"background_only,omitempty" jsonschema:"Default true: pause archive-wide jobs (priority 200+) and leave agent/demand work. False pauses every claimable job of the kind."`
}

func registerMLTools(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	registerTranscriptionTools(srv, dbc)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "enqueue_context_windows", Description: "Queue context-window generation for the current transcript of a video and put it ahead of background demand work. Returns job_id and priority so the agent can reorder. Requires mcp:write."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in *enqueueContextArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(in.VideoID)
		if err != nil {
			return nil, nil, err
		}
		if err := contextwindow.Enqueue(ctx, dbc, id); err != nil {
			return nil, nil, err
		}
		job, err := contextwindow.Job(ctx, dbc, id)
		if err != nil {
			return jsonResult(map[string]string{"status": "queued", "video_id": in.VideoID})
		}
		return jsonResult(mlJobView(job, ""))
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "list_ml_jobs", Description: "Show the ML queue (transcription, context windows, visual index) with priority. Lower priority numbers run first. Use this before set_ml_job_priority or clear_ml_queue."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in *listMLJobsArgs) (*mcpsdk.CallToolResult, any, error) {
		if tokenFrom(ctx) == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 50
		}
		if limit > 200 {
			limit = 200
		}
		rows, err := dbc.Queries(ctx).ListMLQueue(ctx, &db.ListMLQueueParams{Kind: in.Kind, Status: in.Status, RowLimit: limit})
		if err != nil {
			return nil, nil, err
		}
		counts, err := dbc.Queries(ctx).CountMLJobs(ctx)
		if err != nil {
			return nil, nil, err
		}
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			if row == nil {
				continue
			}
			out = append(out, mlQueueRowView(row))
		}
		return jsonResult(map[string]any{"counts": counts, "jobs": out})
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "set_ml_job_priority", Description: "Reorder one claimable ML job. Lower numbers run first. Requires mcp:write."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in *setMLJobPriorityArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(in.JobID)
		if err != nil {
			return nil, nil, err
		}
		if in.Priority < 1 || in.Priority > 1000 {
			return nil, nil, fmt.Errorf("priority must be between 1 and 1000")
		}
		n, err := dbc.Queries(ctx).SetMLJobPriority(ctx, &db.SetMLJobPriorityParams{ID: id, Priority: in.Priority})
		if err != nil {
			return nil, nil, err
		}
		if n == 0 {
			return nil, nil, fmt.Errorf("job is not reorderable (processing or terminal)")
		}
		job, err := dbc.Queries(ctx).GetMLJob(ctx, id)
		if err != nil {
			return jsonResult(map[string]any{"status": "queued", "priority": in.Priority})
		}
		return jsonResult(mlJobView(job, ""))
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "cancel_ml_job", Description: "Cancel one ML job, including an in-flight run. Retry or enqueue_context_windows puts it back. Requires mcp:write."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in *retryMLJobArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(in.JobID)
		if err != nil {
			return nil, nil, err
		}
		n, err := dbc.Queries(ctx).CancelMLJob(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if n == 0 {
			return nil, nil, fmt.Errorf("job is not cancellable")
		}
		return jsonResult(map[string]string{"status": "cancelled", "job_id": in.JobID})
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "clear_ml_queue", Description: "Cancel queued, waiting, and in-flight ML jobs. Default cancels the whole claimable queue. background_only=true limits to archive-wide priority 200+ work. Requires mcp:write."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in *clearMLQueueArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		minPriority := int32(0)
		if in.BackgroundOnly != nil && *in.BackgroundOnly {
			minPriority = 200
		}
		n, err := dbc.Queries(ctx).CancelClaimableMLJobs(ctx, &db.CancelClaimableMLJobsParams{Kind: in.Kind, MinPriority: minPriority})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"status": "cancelled", "cancelled": n, "kind": in.Kind, "min_priority": minPriority})
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "retry_ml_job", Description: "Requeue one failed, waiting, cancelled, or superseded ML job. Requires mcp:write."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in *retryMLJobArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(in.JobID)
		if err != nil {
			return nil, nil, err
		}
		n, err := dbc.Queries(ctx).RetryMLJob(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if n == 0 {
			return nil, nil, fmt.Errorf("job is not retryable")
		}
		return jsonResult(map[string]string{"status": "queued", "job_id": in.JobID})
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "ml_runtime_health", Description: "Show per-kind inference cooldown, failure count, and last error."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		if tokenFrom(ctx) == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		rows, err := dbc.Queries(ctx).ListMLRuntimeHealth(ctx)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(rows)
	})
}

func mlJobView(job *db.MlJob, title string) map[string]any {
	if job == nil {
		return map[string]any{}
	}
	return mlJobFields(job.ID, job.VideoID, job.Kind, job.Status, job.Priority, job.Attempts, job.LastError, title)
}

func mlQueueRowView(row *db.ListMLQueueRow) map[string]any {
	if row == nil {
		return map[string]any{}
	}
	return mlJobFields(row.ID, row.VideoID, row.Kind, row.Status, row.Priority, row.Attempts, row.LastError, row.VideoTitle)
}

func mlJobFields(id, videoID pgtype.UUID, kind, status string, priority, attempts int32, lastError, title string) map[string]any {
	out := map[string]any{
		"job_id":   uuidString(id),
		"video_id": uuidString(videoID),
		"kind":     kind,
		"status":   status,
		"priority": priority,
		"attempts": attempts,
	}
	if title != "" {
		out["title"] = title
	}
	if lastError != "" {
		out["last_error"] = lastError
	}
	return out
}
