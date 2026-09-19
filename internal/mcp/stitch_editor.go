package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/events"
	"thirdcoast.systems/rewind/internal/stitch"
)

type stitchInspectArgs struct {
	ProjectID string `json:"project_id"`
	StartUS   *int64 `json:"start_us,omitempty"`
	EndUS     *int64 `json:"end_us,omitempty"`
}
type stitchApplyArgs struct {
	ProjectID        string             `json:"project_id"`
	ExpectedRevision *int64             `json:"expected_revision"`
	OperationKey     string             `json:"operation_key"`
	Summary          string             `json:"summary"`
	Operations       []stitch.Operation `json:"operations"`
}
type stitchHistoryArgs struct {
	ProjectID     string `json:"project_id"`
	AfterRevision int64  `json:"after_revision"`
	Limit         int    `json:"limit"`
}
type stitchWaitArgs struct {
	ProjectID     string `json:"project_id"`
	AfterRevision int64  `json:"after_revision"`
	TimeoutMS     int    `json:"timeout_ms"`
}
type stitchMoveArgs struct {
	ProjectID        string `json:"project_id"`
	ExpectedRevision *int64 `json:"expected_revision"`
	OperationKey     string `json:"operation_key"`
	Summary          string `json:"summary"`
}

func validateEditorRequest(key, summary string, rev *int64, n int) error {
	if rev == nil {
		return fmt.Errorf("expected_revision is required")
	}
	if len(strings.TrimSpace(key)) < 1 || len(key) > 200 {
		return fmt.Errorf("operation_key must be 1-200 characters")
	}
	if len(summary) > 240 {
		return fmt.Errorf("summary must be at most 240 characters")
	}
	if n < 1 || n > 100 {
		return fmt.Errorf("operations must contain 1-100 items")
	}
	return nil
}

func registerStitchEditorTools(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "stitch_inspect", Description: "Inspect an owned stitch project."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *stitchInspectArgs) (*mcpsdk.CallToolResult, any, error) {
		tok := tokenFrom(ctx)
		if tok == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		id, e := parseUUID(a.ProjectID)
		if e != nil {
			return nil, nil, e
		}
		store := stitch.NewStore(dbc)
		x, e := store.Enable(ctx, tok.UserID, id)
		if e != nil {
			return nil, nil, e
		}
		if (a.StartUS == nil) != (a.EndUS == nil) {
			return nil, nil, fmt.Errorf("start_us and end_us must be provided together")
		}
		if a.StartUS != nil && (*a.StartUS < 0 || *a.EndUS <= *a.StartUS) {
			return nil, nil, fmt.Errorf("invalid inspect range")
		}
		if a.EndUS != nil {
			maxEnd := int64(0)
			for _, seg := range x.Document.Segments {
				if seg.StartUS+seg.DurationUS > maxEnd {
					maxEnd = seg.StartUS + seg.DurationUS
				}
			}
			if *a.EndUS > maxEnd {
				return nil, nil, fmt.Errorf("inspect range exceeds project duration")
			}
		}
		return jsonResult(stitchState(x, a.StartUS, a.EndUS))
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "stitch_apply", Description: "Apply typed operations to an owned stitch project."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *stitchApplyArgs) (*mcpsdk.CallToolResult, any, error) {
		if e := requireWrite(ctx); e != nil {
			return nil, nil, e
		}
		if e := validateEditorRequest(a.OperationKey, a.Summary, a.ExpectedRevision, len(a.Operations)); e != nil {
			return nil, nil, e
		}
		tok := tokenFrom(ctx)
		if tok == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		id, e := parseUUID(a.ProjectID)
		if e != nil {
			return nil, nil, e
		}
		store := stitch.NewStore(dbc)
		if _, e = store.Enable(ctx, tok.UserID, id); e != nil {
			return nil, nil, e
		}
		ac := ActorFrom(ctx)
		r, e := store.Commit(ctx, tok.UserID, id, *a.ExpectedRevision, strings.TrimSpace(a.OperationKey), stitch.Actor{Kind: string(ac.Kind), ID: ac.ID, Name: ac.ClientName}, a.Summary, a.Operations)
		if e != nil {
			if ce, ok := e.(*stitch.ConflictError); ok {
				return &mcpsdk.CallToolResult{IsError: true}, map[string]any{"error": "revision_conflict", "current_revision": ce.CurrentRevision}, nil
			}
			return nil, nil, e
		}
		return jsonResult(stitchResult(r))
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "stitch_history", Description: "Read owned stitch edit history."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *stitchHistoryArgs) (*mcpsdk.CallToolResult, any, error) {
		tok := tokenFrom(ctx)
		if tok == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		id, e := parseUUID(a.ProjectID)
		if e != nil {
			return nil, nil, e
		}
		r, e := stitch.NewStore(dbc).History(ctx, tok.UserID, id, a.AfterRevision, a.Limit)
		if e != nil {
			return nil, nil, e
		}
		return jsonResult(map[string]any{"events": r})
	})
	move := func(redo bool) func(context.Context, *mcpsdk.CallToolRequest, *stitchMoveArgs) (*mcpsdk.CallToolResult, any, error) {
		return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *stitchMoveArgs) (*mcpsdk.CallToolResult, any, error) {
			if e := requireWrite(ctx); e != nil {
				return nil, nil, e
			}
			tok := tokenFrom(ctx)
			if tok == nil {
				return nil, nil, fmt.Errorf("authentication required")
			}
			id, e := parseUUID(a.ProjectID)
			if e != nil {
				return nil, nil, e
			}
			ac := ActorFrom(ctx)
			act := stitch.Actor{Kind: string(ac.Kind), ID: ac.ID, Name: ac.ClientName}
			var r stitch.Result
			if redo {
				if e := validateEditorRequest(a.OperationKey, a.Summary, a.ExpectedRevision, 1); e != nil {
					return nil, nil, e
				}
				r, e = stitch.NewStore(dbc).Redo(ctx, tok.UserID, id, *a.ExpectedRevision, a.OperationKey, act, a.Summary)
			} else {
				if e := validateEditorRequest(a.OperationKey, a.Summary, a.ExpectedRevision, 1); e != nil {
					return nil, nil, e
				}
				r, e = stitch.NewStore(dbc).Undo(ctx, tok.UserID, id, *a.ExpectedRevision, a.OperationKey, act, a.Summary)
			}
			if e != nil {
				if ce, ok := e.(*stitch.ConflictError); ok {
					return &mcpsdk.CallToolResult{IsError: true}, map[string]any{"error": "revision_conflict", "current_revision": ce.CurrentRevision}, nil
				}
				return nil, nil, e
			}
			return jsonResult(stitchResult(r))
		}
	}
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "stitch_undo", Description: "Undo the latest shared Stitch edit."}, move(false))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "stitch_redo", Description: "Redo the latest shared Stitch edit."}, move(true))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "stitch_wait", Description: "Wait for a project change, then reread authoritative state."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *stitchWaitArgs) (*mcpsdk.CallToolResult, any, error) {
		tok := tokenFrom(ctx)
		if tok == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		id, e := parseUUID(a.ProjectID)
		if e != nil {
			return nil, nil, e
		}
		wake, done := events.Default.Subscribe("stitch_projects_changed")
		defer done()
		timeout := time.Duration(a.TimeoutMS) * time.Millisecond
		if timeout <= 0 || timeout > 30*time.Second {
			timeout = 30 * time.Second
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		st := stitch.NewStore(dbc)
		for {
			x, e := st.Get(ctx, tok.UserID, id)
			if e != nil {
				return nil, nil, e
			}
			if x.Revision > a.AfterRevision {
				return jsonResult(stitchState(x, nil, nil))
			}
			select {
			case <-wake:
			case <-timer.C:
				return jsonResult(stitchState(x, nil, nil))
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}
	})
}

func stitchState(x stitch.Snapshot, start, end *int64) map[string]any {
	segments := stitch.Resolve(x.Document)
	captions := stitch.ResolvedCaptions(x.Document)
	overlays := append([]stitch.Overlay(nil), x.Document.Overlays...)
	if start != nil {
		filtered := segments[:0]
		for _, s := range segments {
			if s.EndUS > *start && s.Segment.StartUS < *end {
				filtered = append(filtered, s)
			}
		}
		segments = filtered
		filteredCaps := captions[:0]
		for _, c := range captions {
			if c.EndUS > *start && c.StartUS < *end {
				filteredCaps = append(filteredCaps, c)
			}
		}
		captions = filteredCaps
		filteredOverlays := overlays[:0]
		for _, o := range overlays {
			if o.EndUS > *start && o.StartUS < *end {
				filteredOverlays = append(filteredOverlays, o)
			}
		}
		overlays = filteredOverlays
	}
	matchingIDs := []string{}
	for _, s := range segments {
		matchingIDs = append(matchingIDs, s.Segment.ID)
	}
	for _, c := range captions {
		matchingIDs = append(matchingIDs, c.ID)
	}
	for _, o := range overlays {
		matchingIDs = append(matchingIDs, o.ID)
	}
	return map[string]any{"id": x.ID, "enabled": x.Enabled, "revision": x.Revision, "document": x.Document, "resolved": segments, "resolved_captions": captions, "overlays": overlays, "matching_ids": matchingIDs}
}

func stitchResult(r stitch.Result) map[string]any {
	out := stitchState(r.Snapshot, nil, nil)
	out["changed_ids"] = r.ChangedIDs
	out["edit_id"] = r.EditID
	out["summary"] = r.Summary
	return out
}
