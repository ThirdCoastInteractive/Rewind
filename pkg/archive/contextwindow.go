package archive

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

// ContextWindow is one generated chapter. Shorts are beats nested inside it.
type ContextWindow struct {
	Start, End     float64
	Title, Summary string
	Hook           string
	Shorts         []ContextWindow
}

const (
	importedContextHash   = "archive-import"
	importedContextDigest = "cf/qwen/qwen3.8-27b"
	importedContextPrompt = "context-cues-v1"
	importedContextSource = "cf/qwen/qwen3.8-27b"
)

type contextWindowWriter interface {
	CreateContextWindowSet(context.Context, *db.CreateContextWindowSetParams) (*db.ContextWindowSet, error)
	InsertGeneratedContextWindow(context.Context, *db.InsertGeneratedContextWindowParams) (*db.ContextWindow, error)
	MarkGeneratedWindowsStale(context.Context, *db.MarkGeneratedWindowsStaleParams) error
}

// ImportContextWindows inserts origin=generated windows for videoID.
// Windows with end <= start are skipped. Ordinals are 0..n-1 of the windows
// that remain. created_by is videos.archived_by; a missing user id is returned
// as the insert error and is not replaced with the video id.
func ImportContextWindows(ctx context.Context, videoID string, windows []ContextWindow) error {
	dbc, err := boundDB()
	if err != nil {
		return err
	}
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := importContextWindowsWith(ctx, q, videoID, windows); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func importContextWindowsWith(ctx context.Context, q contextWindowWriter, videoID string, windows []ContextWindow) error {
	id, err := parsePGUUID(videoID)
	if err != nil {
		return err
	}
	kept := contextWindowsToInsert(windows)
	if len(kept) == 0 {
		return nil
	}
	set, err := q.CreateContextWindowSet(ctx, &db.CreateContextWindowSetParams{
		VideoID:        id,
		TranscriptHash: importedContextHash,
		ModelDigest:    importedContextDigest,
		PromptVersion:  importedContextPrompt,
		Status:         "succeeded",
		Metrics:        []byte(`{}`),
	})
	if err != nil {
		return fmt.Errorf("archive: context window set: %w", err)
	}
	if set == nil {
		return fmt.Errorf("archive: context window set missing")
	}
	if err := q.MarkGeneratedWindowsStale(ctx, &db.MarkGeneratedWindowsStaleParams{
		VideoID: id, ExceptSetID: set.ID,
	}); err != nil {
		return fmt.Errorf("archive: stale previous context windows: %w", err)
	}
	shortN := 0
	for i, w := range kept {
		parent, err := q.InsertGeneratedContextWindow(ctx, generatedWindowParams(id, set.ID, w, "window", int32(i), pgtype.UUID{}))
		if err != nil {
			return fmt.Errorf("archive: insert context window: %w", err)
		}
		parentID := parentIDOf(parent)
		for _, s := range w.Shorts {
			if s.End <= s.Start {
				continue
			}
			shortN++
			if _, err := q.InsertGeneratedContextWindow(ctx, generatedWindowParams(id, set.ID, s, "short", 10000+int32(shortN), parentID)); err != nil {
				return fmt.Errorf("archive: insert context short: %w", err)
			}
		}
	}
	return nil
}

func parentIDOf(row *db.ContextWindow) pgtype.UUID {
	if row == nil {
		return pgtype.UUID{}
	}
	return row.ID
}

func generatedWindowParams(videoID, setID pgtype.UUID, w ContextWindow, kind string, ordinal int32, parent pgtype.UUID) *db.InsertGeneratedContextWindowParams {
	summary := w.Summary
	if kind == "short" && summary == "" {
		summary = w.Hook
	}
	return &db.InsertGeneratedContextWindowParams{
		VideoID:               videoID,
		StartTs:               w.Start,
		EndTs:                 w.End,
		Title:                 w.Title,
		Summary:               summary,
		Topics:                []string{},
		Entities:              []string{},
		SourceQuery:           importedContextSource,
		TranscriptCueEvidence: []byte(`[]`),
		BoundaryQuality:       "cue",
		SetID:                 setID,
		Ordinal:               ordinal,
		Kind:                  kind,
		ParentID:              parent,
		Hook:                  w.Hook,
	}
}

func contextWindowsToInsert(in []ContextWindow) []ContextWindow {
	kept := make([]ContextWindow, 0, len(in))
	for _, w := range in {
		if w.End <= w.Start {
			continue
		}
		kept = append(kept, w)
	}
	return kept
}
