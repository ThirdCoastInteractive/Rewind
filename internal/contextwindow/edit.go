// Package contextwindow applies the same edit semantics to HTTP and MCP clients.
package contextwindow

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"thirdcoast.systems/rewind/internal/db"
)

// Patch distinguishes omitted fields from explicit zero, empty text, and empty arrays.
type Patch struct {
	Start           *float64 `json:"start,omitempty"`
	End             *float64 `json:"end,omitempty"`
	Title           *string  `json:"title,omitempty"`
	Summary         *string  `json:"summary,omitempty"`
	Topics          []string `json:"topics,omitempty"`
	Entities        []string `json:"entities,omitempty"`
	BoundaryQuality *string  `json:"boundary_quality,omitempty"`
}

// Edit locks the row, validates against the video, and preserves omitted fields.
func Edit(ctx context.Context, dbc *db.DatabaseConnection, id pgtype.UUID, p Patch) (*db.ContextWindow, error) {
	q, tx, e := dbc.NewWithTX(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = q.LockContextWindow(ctx, id); e != nil {
		return nil, e
	}
	old, e := q.GetContextWindow(ctx, id)
	if e != nil {
		return nil, e
	}
	a := &db.UpdateContextWindowParams{ID: id, StartTs: old.StartTs, EndTs: old.EndTs, Title: old.Title, Summary: old.Summary, Topics: old.Topics, Entities: old.Entities, BoundaryQuality: old.BoundaryQuality}
	if p.Start != nil {
		a.StartTs = *p.Start
	}
	if p.End != nil {
		a.EndTs = *p.End
	}
	if p.Title != nil {
		a.Title = strings.TrimSpace(*p.Title)
	}
	if p.Summary != nil {
		a.Summary = *p.Summary
	}
	if p.Topics != nil {
		a.Topics = p.Topics
	}
	if p.Entities != nil {
		a.Entities = p.Entities
	}
	if p.BoundaryQuality != nil {
		a.BoundaryQuality = *p.BoundaryQuality
	}
	v, e := q.GetVideoByID(ctx, old.VideoID)
	if e != nil {
		return nil, e
	}
	if e = Validate(a.StartTs, a.EndTs, a.Title, v.DurationSeconds); e != nil {
		return nil, e
	}
	row, e := q.UpdateContextWindow(ctx, a)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return row, nil
}
