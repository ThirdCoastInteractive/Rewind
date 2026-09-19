package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

type stitchRenderArgs struct {
	ProjectID    string `json:"project_id"`
	Revision     *int64 `json:"revision"`
	OperationKey string `json:"operation_key"`
	FrameTimeUS  *int64 `json:"frame_time_us"`
	stitch.RenderOptions
}
type stitchRenderStatusArgs struct {
	ProjectID string `json:"project_id"`
	JobID     string `json:"job_id"`
}

func mcpRenderJobResponse(job stitch.RenderJob) map[string]any {
	result := map[string]any{"id": job.ID, "project_id": job.ProjectID, "revision": job.Revision, "kind": job.Kind, "status": job.Status, "options": job.Options, "mime": job.MIME, "progress": job.Progress}
	if job.AssetURL != "" {
		result["asset_url"] = "/api/stitch/" + job.ID.String() + "/stream"
	}
	if len(job.SidecarURLs) > 0 {
		result["sidecar_urls"] = job.SidecarURLs
	}
	if job.Error != "" {
		result["error"] = job.Error
	}
	return result
}

func registerStitchEditorRenderTools(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "stitch_export", Description: "Queue an immutable export of an owned Stitch project at an explicit revision. Requires mcp:write; this edits no document."}, stitchExportMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "stitch_preview", Description: "Queue a bounded preview of an owned Stitch project revision. Requires mcp:write."}, stitchPreviewMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "stitch_frame", Description: "Queue one rendered frame at an explicit timestamp for an owned Stitch revision. Requires mcp:write."}, stitchFrameMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "stitch_render_status", Description: "Read minimal status for an owned Stitch render job. Requires mcp:read."}, stitchRenderStatusMCP(dbc))
}

func queueRenderMCP(ctx context.Context, a *stitchRenderArgs, dbc *db.DatabaseConnection, kind string) (*mcpsdk.CallToolResult, any, error) {
	if err := requireWrite(ctx); err != nil {
		return nil, nil, err
	}
	if a == nil || a.Revision == nil || *a.Revision < 0 || strings.TrimSpace(a.OperationKey) == "" || len(a.OperationKey) > 200 {
		return nil, nil, fmt.Errorf("project_id, revision, and bounded operation_key are required")
	}
	project, err := uuid.Parse(a.ProjectID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid project_id")
	}
	if kind == "frame" {
		if a.FrameTimeUS == nil || *a.FrameTimeUS < 0 {
			return nil, nil, fmt.Errorf("frame_time_us is required")
		}
		a.RenderOptions.FrameTimeUS = *a.FrameTimeUS
	}
	owner := tokenFrom(ctx).UserID
	pid := pgtype.UUID{Bytes: project, Valid: true}
	store := stitch.NewStore(dbc)
	if _, err = store.Enable(ctx, owner, pid); err != nil {
		return nil, nil, err
	}
	var job stitch.RenderJob
	switch kind {
	case "preview":
		job, err = store.QueuePreview(ctx, owner, pid, *a.Revision, a.OperationKey, a.RenderOptions)
	case "frame":
		job, err = store.QueueFrame(ctx, owner, pid, *a.Revision, a.OperationKey, a.RenderOptions)
	default:
		job, err = store.QueueExport(ctx, owner, pid, *a.Revision, a.OperationKey, a.RenderOptions)
	}
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(mcpRenderJobResponse(job))
}

func stitchPreviewMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *stitchRenderArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *stitchRenderArgs) (*mcpsdk.CallToolResult, any, error) {
		return queueRenderMCP(ctx, a, dbc, "preview")
	}
}

func stitchFrameMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *stitchRenderArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *stitchRenderArgs) (*mcpsdk.CallToolResult, any, error) {
		return queueRenderMCP(ctx, a, dbc, "frame")
	}
}

func stitchExportMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *stitchRenderArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *stitchRenderArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		if a == nil || a.Revision == nil || *a.Revision < 0 || strings.TrimSpace(a.OperationKey) == "" || len(a.OperationKey) > 200 {
			return nil, nil, fmt.Errorf("project_id, revision, and bounded operation_key are required")
		}
		project, err := uuid.Parse(a.ProjectID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid project_id")
		}
		owner := tokenFrom(ctx).UserID
		pid := pgtype.UUID{Bytes: project, Valid: true}
		store := stitch.NewStore(dbc)
		if _, err = store.Enable(ctx, owner, pid); err != nil {
			return nil, nil, err
		}
		job, err := store.QueueExport(ctx, owner, pid, *a.Revision, a.OperationKey, a.RenderOptions)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(mcpRenderJobResponse(job))
	}
}

func stitchRenderStatusMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *stitchRenderStatusArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *stitchRenderStatusArgs) (*mcpsdk.CallToolResult, any, error) {
		if tokenFrom(ctx) == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		if a == nil {
			return nil, nil, fmt.Errorf("project_id and job_id are required")
		}
		project, err := uuid.Parse(a.ProjectID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid project_id")
		}
		jobID, err := uuid.Parse(a.JobID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid job_id")
		}
		job, err := stitch.NewStore(dbc).GetRenderJob(ctx, tokenFrom(ctx).UserID, pgtype.UUID{Bytes: jobID, Valid: true})
		if err != nil {
			return nil, nil, err
		}
		if job.ProjectID.Bytes != project || !job.ProjectID.Valid {
			return nil, nil, fmt.Errorf("render job does not belong to project")
		}
		return jsonResult(mcpRenderJobResponse(job))
	}
}
