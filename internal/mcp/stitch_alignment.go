package mcp

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

type stitchAlignmentRequestArgs struct {
	ProjectID    string `json:"project_id"`
	CaptionID    string `json:"caption_id"`
	Revision     *int64 `json:"revision"`
	OperationKey string `json:"operation_key"`
	Language     string `json:"language"`
	ModelVersion string `json:"model_version"`
}
type stitchAlignmentStatusArgs struct {
	ProjectID string `json:"project_id"`
	JobID     string `json:"job_id"`
}

func registerStitchAlignmentTools(srv *mcp.Server, dbc *db.DatabaseConnection) {
	mcp.AddTool(srv, &mcp.Tool{Name: "request_stitch_alignment", Description: "Queue owned WhisperX caption alignment with an expected project revision."}, requestStitchAlignment(dbc))
	mcp.AddTool(srv, &mcp.Tool{Name: "get_stitch_alignment_status", Description: "Read an owned Stitch alignment job status and runtime result."}, getStitchAlignmentStatus(dbc))
}

func requestStitchAlignment(dbc *db.DatabaseConnection) func(context.Context, *mcp.CallToolRequest, *stitchAlignmentRequestArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, a *stitchAlignmentRequestArgs) (*mcp.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		tok := tokenFrom(ctx)
		if tok == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		project, err := parseUUID(a.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		if a.Revision == nil || a.OperationKey == "" {
			return nil, nil, fmt.Errorf("revision and operation_key are required")
		}
		actorInfo := ActorFrom(ctx)
		actor := stitch.Actor{Kind: string(actorInfo.Kind), ID: actorInfo.ID, Name: "mcp"}
		r, err := stitch.NewStore(dbc).QueueAlignment(ctx, tok.UserID, project, *a.Revision, a.OperationKey, actor, a.CaptionID, a.Language, a.ModelVersion)
		if err != nil {
			return nil, nil, err
		}
		key := findAlignmentKey(r.Document, a.CaptionID)
		job, _ := stitch.NewStore(dbc).AlignmentStatusByKey(ctx, tok.UserID, project, key)
		return jsonResult(map[string]any{"result": r, "job_id": job.ID.String(), "caption_id": a.CaptionID, "alignment_key": key})
	}
}

func getStitchAlignmentStatus(dbc *db.DatabaseConnection) func(context.Context, *mcp.CallToolRequest, *stitchAlignmentStatusArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, a *stitchAlignmentStatusArgs) (*mcp.CallToolResult, any, error) {
		if tokenFrom(ctx) == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		tok := tokenFrom(ctx)
		if tok == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		project, err := parseUUID(a.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		job, err := parseUUID(a.JobID)
		if err != nil {
			return nil, nil, err
		}
		j, err := stitch.NewStore(dbc).AlignmentStatus(ctx, tok.UserID, project, job)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(j)
	}
}

func findAlignmentKey(d stitch.Document, id string) string {
	for _, c := range d.Captions {
		if c.ID == id {
			return c.AlignmentKey
		}
	}
	return ""
}
