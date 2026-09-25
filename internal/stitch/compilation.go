package stitch

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// QueueCompilationRender snapshots the initialized canonical document inside
// the compilation transaction. The returned job is therefore tied to the
// exact document that was created, rather than rereading mutable legacy JSON.
func QueueCompilationRender(ctx context.Context, tx pgx.Tx, owner, project pgtype.UUID, revision int64, key string, doc Document) (pgtype.UUID, error) {
	if key == "" || len(key) > 200 {
		return pgtype.UUID{}, fmt.Errorf("invalid operation key")
	}
	var persisted []byte
	var currentRevision int64
	var title string
	if err := tx.QueryRow(ctx, `SELECT document,revision,title FROM stitch_projects WHERE id=$1 AND created_by=$2 FOR UPDATE`, project, owner).Scan(&persisted, &currentRevision, &title); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, ErrNotFound
		}
		return pgtype.UUID{}, err
	}
	if len(persisted) == 0 {
		return pgtype.UUID{}, ErrNotFound
	}
	requestPayload, _ := json.Marshal(struct {
		Kind        string `json:"kind"`
		ProjectID   string `json:"project_id"`
		Revision    int64  `json:"revision"`
		Format      string `json:"format"`
		Quality     string `json:"quality"`
		CaptionMode string `json:"caption_mode"`
		Scope       string `json:"scope"`
	}{"compilation", project.String(), revision, "mp4", "high", "none", "all"})
	hash := sha256.Sum256(requestPayload)
	requestHash := fmt.Sprintf("%x", hash[:])
	var job pgtype.UUID
	var originalHash string
	if err := tx.QueryRow(ctx, `SELECT id,render_request_hash FROM stitch_jobs WHERE project_id=$1 AND export_operation_key=$2`, project, key).Scan(&job, &originalHash); err == nil {
		if originalHash != requestHash {
			return pgtype.UUID{}, ErrIdempotency
		}
		return job, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, err
	}
	// Reuse an in-flight export for the same project title even if the key differs.
	if err := tx.QueryRow(ctx, `SELECT id FROM stitch_jobs WHERE project_id=$1 AND title=$2 AND COALESCE(render_kind,'export')='export' AND status IN ('queued','processing') ORDER BY created_at ASC LIMIT 1 FOR UPDATE`, project, title).Scan(&job); err == nil {
		return job, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, err
	}
	if currentRevision != revision {
		return pgtype.UUID{}, &ConflictError{CurrentRevision: currentRevision}
	}
	// The database document is authoritative. The caller's legacy or stale
	// payload must never become the render snapshot.
	var canonical Document
	var err error
	if err := json.Unmarshal(persisted, &canonical); err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid persisted document: %w", err)
	}
	if err := Validate(canonical); err != nil {
		return pgtype.UUID{}, err
	}
	canonical, err = validateSources(ctx, tx, owner, canonical)
	if err != nil {
		return pgtype.UUID{}, err
	}
	options := RenderOptions{Format: "mp4", Quality: "high", Scope: "all", CaptionMode: "none"}
	envelope, err := BuildRenderSnapshotWithAssets(ctx, tx, owner, project, canonical, options)
	if err != nil {
		return pgtype.UUID{}, err
	}
	snapshot, err := json.Marshal(envelope)
	if err != nil {
		return pgtype.UUID{}, err
	}
	optionsJSON, _ := json.Marshal(options)
	err = tx.QueryRow(ctx, `INSERT INTO stitch_jobs(created_by,title,format,quality,segments,global_filters,project_id,export_operation_key,render_kind,project_revision,document_snapshot,render_options,render_request_hash) VALUES($1,$2,'mp4','high','[]','[]',$3,$4,'export',$5,$6,$7,$8) RETURNING id`, owner, title, project, key, revision, snapshot, optionsJSON, requestHash).Scan(&job)
	if err != nil {
		return pgtype.UUID{}, err
	}
	_, err = tx.Exec(ctx, `SELECT pg_notify('stitch_jobs_changed',$1)`, job.String())
	return job, err
}

// InitializeFromLegacy converts a compilation payload while it is still inside
// the caller's transaction. The legacy fields remain intact for old renders.
// Empty canonical shells from CreateStitchProject may be replaced once.
func InitializeFromLegacy(ctx context.Context, tx pgx.Tx, owner, project pgtype.UUID, title, format, quality string, segments, filters json.RawMessage) (Document, error) {
	var existing []byte
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT document, editor_enabled FROM stitch_projects WHERE id=$1 AND created_by=$2 FOR UPDATE`, project, owner).Scan(&existing, &enabled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Document{}, ErrNotFound
		}
		return Document{}, err
	}
	_ = enabled
	if len(existing) != 0 {
		var shell Document
		if err := json.Unmarshal(existing, &shell); err != nil || len(shell.Segments) > 0 || len(shell.Captions) > 0 || len(shell.Overlays) > 0 {
			return Document{}, fmt.Errorf("stitch project is already canonical")
		}
	}
	originalSegments := append([]byte(nil), segments...)
	var err error
	segments, err = hydrateLegacySegments(ctx, tx, owner, segments)
	if err != nil {
		return Document{}, err
	}
	doc, err := FromLegacy(title, format, quality, segments, filters)
	if err != nil {
		return Document{}, err
	}
	doc, err = validateSources(ctx, tx, owner, doc)
	if err != nil {
		return Document{}, err
	}
	document, err := json.Marshal(doc)
	if err != nil {
		return Document{}, err
	}
	legacySnapshot, err := json.Marshal(struct {
		Title    string          `json:"title"`
		Format   string          `json:"format"`
		Quality  string          `json:"quality"`
		Segments json.RawMessage `json:"segments"`
		Filters  json.RawMessage `json:"global_filters"`
	}{title, format, quality, json.RawMessage(originalSegments), filters})
	if err != nil {
		return Document{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE stitch_projects SET title=$3, format=$4, quality=$5, segments=$6, global_filters=$7, document=$8, legacy_snapshot=COALESCE(legacy_snapshot,$9), document_version=1, editor_enabled=true, updated_at=now() WHERE id=$1 AND created_by=$2`, project, owner, title, format, quality, segments, filters, document, legacySnapshot)
	if err != nil {
		return Document{}, err
	}
	return doc, nil
}
