package mcp

import (
	"context"
	"fmt"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/vision"
)

func registerVisionTools(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "index_visual_range", Description: "Queue a video or range for visual indexing; dense=true adds one-second samples. Requires mcp:write."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in *vision.IndexRange) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := vision.QueueRange(ctx, dbc, *in)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"job_id": id.String()})
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "visual_index_status", Description: "Show visual indexing progress, including missing-asset errors."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		if tokenFrom(ctx) == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		rows, e := dbc.Queries(ctx).ListVisualProgress(ctx)
		if e != nil {
			return nil, nil, e
		}
		return jsonResult(rows)
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "search_visual_moments", Description: "Find timestamped images using CLIP text, an uploaded reference_id, or a frame_ref. Inspect returned frames before drawing conclusions. Scores are similarities, not probabilities."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in *vision.SearchInput) (*mcpsdk.CallToolResult, any, error) {
		tok := tokenFrom(ctx)
		if tok == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		rows, e := vision.Search(ctx, dbc, tok.UserID, *in)
		if e != nil {
			return nil, nil, e
		}
		return jsonResult(rows)
	})
}
