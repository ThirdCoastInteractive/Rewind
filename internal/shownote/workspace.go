package shownote

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"thirdcoast.systems/rewind/internal/db"
)

// MigrationReport summarizes an idempotent workspace backfill/preflight run.
type MigrationReport struct {
	Total     int      `json:"total"`
	Migrated  int      `json:"migrated"`
	Failed    int      `json:"failed"`
	Pending   int      `json:"pending"`
	Failures  []string `json:"failures,omitempty"`
	CanEnable bool     `json:"can_enable"`
}

// EnsureWorkspaceDocument transactionally converts one legacy block tree into
// Markdown and its initial Yjs state. Existing documents are never overwritten.
func EnsureWorkspaceDocument(ctx context.Context, dbc *db.DatabaseConnection, noteID pgtype.UUID) error {
	if !noteID.Valid {
		return errors.New("show-note id is required")
	}
	tx, err := dbc.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin workspace migration: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	txq := db.New(tx)
	if _, err := txq.LockShowNoteForWorkspaceMigration(ctx, noteID); err != nil {
		return fmt.Errorf("lock show note for migration: %w", err)
	}
	exists, err := txq.ShowNoteDocumentExists(ctx, noteID)
	if err != nil {
		return err
	}
	if exists {
		if err := txq.MarkShowNoteWorkspaceMigrated(ctx, noteID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	blocks, err := txq.ListBlocksForShowNote(ctx, noteID)
	if err != nil {
		return fmt.Errorf("load legacy show-note blocks: %w", err)
	}
	clips := make(map[string]*db.Clip)
	for _, block := range blocks {
		if !block.ClipID.Valid {
			continue
		}
		clip, err := txq.GetClip(ctx, block.ClipID)
		if err == nil {
			clips[block.ClipID.String()] = clip
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("load legacy clip %s: %w", block.ClipID.String(), err)
		}
	}
	markdown := LegacyMarkdown(blocks, clips)
	if len(blocks) == 0 {
		markdown = "# Rundown\n\nStart typing, paste a video link, or use /video, /clip, /marker, or /break.\n"
	}
	snapshot := EncodeInitialDocument(markdown)
	if _, err := txq.CreateShowNoteDocument(ctx, &db.CreateShowNoteDocumentParams{
		ShowNoteID: noteID, Markdown: markdown, Revision: 0,
		Snapshot: snapshot, SnapshotRevision: 0,
	}); err != nil {
		return fmt.Errorf("create show-note document: %w", err)
	}
	if err := replaceReferenceProjection(ctx, txq, noteID, 0, ParseMarkdown(markdown)); err != nil {
		return err
	}
	if err := txq.MarkShowNoteWorkspaceMigrated(ctx, noteID); err != nil {
		return fmt.Errorf("mark show-note migrated: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit workspace migration: %w", err)
	}
	return nil
}

// MigrateAllWorkspaces converts all pending notes and then runs the release
// preflight that requires every note to have a document and matching projection.
func MigrateAllWorkspaces(ctx context.Context, dbc *db.DatabaseConnection) MigrationReport {
	report := MigrationReport{}
	q := dbc.Queries(ctx)
	ids, err := q.ListAllShowNoteIDs(ctx)
	if err != nil {
		report.Failed = 1
		report.Failures = []string{err.Error()}
		return report
	}
	report.Total = len(ids)
	for _, id := range ids {
		if err := EnsureWorkspaceDocument(ctx, dbc, id); err != nil {
			report.Failed++
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", id.String(), err))
			_ = q.MarkShowNoteWorkspaceMigrationFailed(ctx, &db.MarkShowNoteWorkspaceMigrationFailedParams{
				ID: id, WorkspaceMigrationError: err.Error(),
			})
			continue
		}
		report.Migrated++
	}
	pending, err := q.CountPendingWorkspaceMigrations(ctx)
	if err != nil {
		report.Failed++
		report.Failures = append(report.Failures, err.Error())
	} else {
		report.Pending = int(pending)
	}
	report.CanEnable = report.Failed == 0 && report.Pending == 0
	return report
}

// WorkspacePreflight verifies migration coverage and that the stored Markdown
// reference order matches the persisted reference projection.
func WorkspacePreflight(ctx context.Context, dbc *db.DatabaseConnection) MigrationReport {
	report := MigrationReport{}
	q := dbc.Queries(ctx)
	rows, err := q.ListWorkspacePreflightDocuments(ctx)
	if err != nil {
		report.Failed = 1
		report.Failures = []string{err.Error()}
		return report
	}
	for _, row := range rows {
		report.Total++
		if row.Markdown == nil || row.Revision == nil {
			report.Pending++
			continue
		}
		parsed := ParseMarkdown(*row.Markdown)
		count, err := q.CountShowNoteReferencesAtRevision(ctx, &db.CountShowNoteReferencesAtRevisionParams{
			ShowNoteID: row.ID, ParsedRevision: *row.Revision,
		})
		if err != nil {
			report.Failed++
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", row.ID.String(), err))
			continue
		}
		if count != int64(len(parsed.References)) {
			report.Failed++
			report.Failures = append(report.Failures, fmt.Sprintf("%s: parsed %d references, stored %d", row.ID.String(), len(parsed.References), count))
			continue
		}
		stored, err := q.ListShowNoteReferences(ctx, row.ID)
		if err != nil {
			report.Failed++
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", row.ID.String(), err))
			continue
		}
		mismatch := ""
		for index, reference := range parsed.References {
			persisted := stored[index]
			startMatches := reference.Bounds.HasTime == (persisted.StartSeconds != nil)
			if startMatches && reference.Bounds.HasTime {
				startMatches = *persisted.StartSeconds == reference.Bounds.Start
			}
			endMatches := reference.Bounds.IsRange == (persisted.EndSeconds != nil)
			if endMatches && reference.Bounds.IsRange {
				endMatches = *persisted.EndSeconds == reference.Bounds.End
			}
			if persisted.Ordinal != int32(reference.Ordinal) || persisted.OccurrenceKey != reference.OccurrenceKey || persisted.SourceUri != reference.URI || persisted.Kind != string(reference.Kind) || !startMatches || !endMatches {
				mismatch = fmt.Sprintf("reference %d does not round-trip", index+1)
				break
			}
		}
		if mismatch != "" {
			report.Failed++
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %s", row.ID.String(), mismatch))
			continue
		}
		report.Migrated++
	}
	report.CanEnable = report.Failed == 0 && report.Pending == 0
	return report
}
