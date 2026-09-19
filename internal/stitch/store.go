// Package stitch provides durable state for the collaborative stitch editor.
package stitch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

var (
	// ErrNotFound indicates that a project does not exist for the owner.
	ErrNotFound = errors.New("stitch project not found")
	// ErrConflict indicates that the client's revision is stale.
	ErrConflict = errors.New("stitch project revision conflict")
	// ErrIdempotency indicates reuse of an operation key with different input.
	ErrIdempotency = errors.New("stitch operation key was reused with different input")
	// ErrDisabled is retained for callers that still map the legacy enable
	// conflict; store read/write paths no longer return it as a product flag.
	ErrDisabled = errors.New("stitch editor is disabled")
)

// ConflictError reports the revision observed while rejecting a stale write.
type ConflictError struct{ CurrentRevision int64 }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("stitch revision conflict: current revision %d", e.CurrentRevision)
}
func (e *ConflictError) Unwrap() error { return ErrConflict }

// Snapshot is the durable editor state at one project revision.
type Snapshot struct {
	ID          pgtype.UUID `json:"id"`
	Revision    int64       `json:"revision"`
	Enabled     bool        `json:"enabled"`
	Document    Document    `json:"document"`
	Description string      `json:"description"`
	Tags        []string    `json:"tags"`
}

// Result describes a committed editor operation.
type Result struct {
	Snapshot
	ChangedIDs []string    `json:"changed_ids"`
	EditID     pgtype.UUID `json:"edit_id"`
	Summary    string      `json:"summary"`
}

// Event is an immutable entry in the project edit history.
type Event struct {
	ID         pgtype.UUID        `json:"id"`
	Revision   int64              `json:"revision"`
	Actor      Actor              `json:"actor"`
	Summary    string             `json:"summary"`
	Kind       string             `json:"kind"`
	ChangedIDs []string           `json:"changed_ids"`
	Operations []Operation        `json:"operations"`
	CreatedAt  pgtype.Timestamptz `json:"created_at"`
}

// Store persists validated canonical editor documents and their history.
type Store struct{ db *db.DatabaseConnection }

// NewStore returns a store backed by the database connection.
func NewStore(database *db.DatabaseConnection) *Store { return &Store{db: database} }

func uuidText(u pgtype.UUID) string { return u.String() }

func decodeDocument(raw []byte) (Document, error) {
	var d Document
	if len(raw) == 0 {
		return d, errors.New("empty stitch document")
	}
	return d, json.Unmarshal(raw, &d)
}
func hashRequest(value any) (string, []byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), b, nil
}

func (s *Store) snapshot(ctx context.Context, tx pgx.Tx, owner, project pgtype.UUID, lock bool) (Snapshot, []pgtype.UUID, []pgtype.UUID, error) {
	q := `SELECT id, revision, editor_enabled, document, undo_stack, redo_stack, description, tags FROM stitch_projects WHERE id=$1`
	args := []any{project}
	if lock {
		q += ` AND created_by=$2 FOR UPDATE`
		args = append(args, owner)
	}
	var id pgtype.UUID
	var rev int64
	var enabled bool
	var raw []byte
	var undo, redo []pgtype.UUID
	var description string
	var tags []string
	if err := tx.QueryRow(ctx, q, args...).Scan(&id, &rev, &enabled, &raw, &undo, &redo, &description, &tags); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Snapshot{}, nil, nil, ErrNotFound
		}
		return Snapshot{}, nil, nil, err
	}
	_ = enabled // column retained; canonical presence is document, not the flag
	if tags == nil {
		tags = []string{}
	}
	var d Document
	if len(raw) > 0 {
		var decodeErr error
		d, decodeErr = decodeDocument(raw)
		if decodeErr != nil {
			return Snapshot{}, nil, nil, decodeErr
		}
	}
	return Snapshot{ID: id, Revision: rev, Enabled: len(raw) > 0, Document: d, Description: description, Tags: tags}, undo, redo, nil
}

// Get returns the current project snapshot. Rows without a document are
// one-way migrated from legacy segments JSON.
func (s *Store) Get(ctx context.Context, ownerID, projectID pgtype.UUID) (Snapshot, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snap, _, _, err := s.snapshot(ctx, tx, ownerID, projectID, false)
	_ = tx.Rollback(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if !snap.Enabled {
		return s.Enable(ctx, ownerID, projectID)
	}
	return snap, nil
}

// Enable one-way imports old segments JSON into a canonical document for
// existing rows. Projects that already have a document are returned as-is.
func (s *Store) Enable(ctx context.Context, ownerID, projectID pgtype.UUID) (Snapshot, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	var enabled bool
	var revision int64
	var title, format, quality, description string
	var tags []string
	var segments, filters []byte
	err = tx.QueryRow(ctx, `SELECT editor_enabled, document, revision, title, format, quality, segments, global_filters, description, tags FROM stitch_projects WHERE id=$1 AND created_by=$2 FOR UPDATE`, projectID, ownerID).Scan(&enabled, &raw, &revision, &title, &format, &quality, &segments, &filters, &description, &tags)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, err
	}
	if tags == nil {
		tags = []string{}
	}
	if len(raw) != 0 {
		var d Document
		if err := json.Unmarshal(raw, &d); err != nil {
			return Snapshot{}, err
		}
		if !enabled {
			if _, err = tx.Exec(ctx, `UPDATE stitch_projects SET editor_enabled=true, updated_at=now() WHERE id=$1 AND created_by=$2`, projectID, ownerID); err != nil {
				return Snapshot{}, err
			}
			if err = tx.Commit(ctx); err != nil {
				return Snapshot{}, err
			}
			return Snapshot{ID: projectID, Revision: revision, Enabled: true, Document: d, Description: description, Tags: tags}, nil
		}
		snap, _, _, snapErr := s.snapshot(ctx, tx, ownerID, projectID, false)
		return snap, snapErr
	}
	originalSegments := append([]byte(nil), segments...)
	segments, err = hydrateLegacySegments(ctx, tx, ownerID, segments)
	if err != nil {
		return Snapshot{}, err
	}
	d, err := FromLegacy(title, format, quality, segments, filters)
	if err != nil {
		return Snapshot{}, err
	}
	var legacyRows []map[string]json.RawMessage
	if json.Unmarshal(segments, &legacyRows) == nil {
		for i := range d.Segments {
			if i < len(legacyRows) {
				d.Segments[i].Legacy, _ = json.Marshal(legacyRows[i])
			}
		}
	}
	d, err = validateSources(ctx, tx, ownerID, d)
	if err != nil {
		return Snapshot{}, err
	}
	b, err := json.Marshal(d)
	if err != nil {
		return Snapshot{}, err
	}
	legacySnapshot, _ := json.Marshal(struct {
		Title    string          `json:"title"`
		Format   string          `json:"format"`
		Quality  string          `json:"quality"`
		Segments json.RawMessage `json:"segments"`
		Filters  json.RawMessage `json:"global_filters"`
	}{title, format, quality, json.RawMessage(originalSegments), filters})
	if _, err = tx.Exec(ctx, `UPDATE stitch_projects SET document=$3, legacy_snapshot=COALESCE(legacy_snapshot,$4), document_version=1, editor_enabled=true, revision=0, undo_stack='{}', redo_stack='{}', updated_at=now() WHERE id=$1 AND created_by=$2`, projectID, ownerID, b, legacySnapshot); err != nil {
		return Snapshot{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_notify('stitch_projects_changed', $1)`, uuidText(projectID)); err != nil {
		return Snapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ID: projectID, Enabled: true, Document: d, Description: description, Tags: tags}, nil
}

func (s *Store) commit(ctx context.Context, owner, project pgtype.UUID, expected int64, key string, actor Actor, summary, kind string, ops []Operation) (Result, error) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(actor.Kind) == "" || strings.TrimSpace(actor.ID) == "" {
		return Result{}, errors.New("stitch operation key and actor are required")
	}
	if len(ops) > 100 {
		return Result{}, fmt.Errorf("stitch operation batch exceeds 100 operations")
	}
	hash, _, err := hashRequest(struct {
		Kind     string      `json:"kind"`
		Expected int64       `json:"expected_revision"`
		Summary  string      `json:"summary"`
		Ops      []Operation `json:"operations"`
	}{kind, expected, summary, ops})
	if err != nil {
		return Result{}, err
	}
	opJSON, err := json.Marshal(ops)
	if err != nil {
		return Result{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)
	snap, _, _, err := s.snapshot(ctx, tx, owner, project, true)
	if err != nil {
		return Result{}, err
	}
	var existing pgtype.UUID
	var exHash string
	var exRev int64
	var exRaw []byte
	var exChanged []byte
	var exSummary string
	err = tx.QueryRow(ctx, `SELECT e.id, e.request_hash, e.revision, e.after_document, e.changed_ids, e.summary FROM stitch_edits e JOIN stitch_projects p ON p.id=e.project_id WHERE e.project_id=$1 AND p.created_by=$2 AND e.operation_key=$3`, project, owner, key).Scan(&existing, &exHash, &exRev, &exRaw, &exChanged, &exSummary)
	if err == nil {
		if exHash != hash {
			return Result{}, ErrIdempotency
		}
		d, e := decodeDocument(exRaw)
		if e != nil {
			return Result{}, e
		}
		var ids []string
		_ = json.Unmarshal(exChanged, &ids)
		return Result{Snapshot: Snapshot{ID: project, Revision: exRev, Enabled: true, Document: d, Description: snap.Description, Tags: snap.Tags}, ChangedIDs: ids, EditID: existing, Summary: exSummary}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, err
	}
	if !snap.Enabled {
		return Result{}, ErrNotFound
	}
	if snap.Revision != expected {
		return Result{}, &ConflictError{CurrentRevision: snap.Revision}
	}
	for _, op := range ops {
		if op.Type == "insert_segment" && op.Segment != nil && len(op.Segment.Legacy) > 0 {
			var raw map[string]json.RawMessage
			if json.Unmarshal(op.Segment.Legacy, &raw) == nil && len(raw["legacy_metadata"]) > 0 {
				return Result{}, fmt.Errorf("legacy_metadata is server-owned and cannot be supplied")
			}
		}
	}
	ops, err = prepareImportOperations(ctx, tx, snap.Document, ops)
	if err != nil {
		return Result{}, err
	}
	before, _ := json.Marshal(snap.Document)
	afterDoc, changed, err := Apply(snap.Document, ops)
	if err != nil {
		return Result{}, err
	}
	for _, op := range ops {
		if op.Type == "request_alignment" {
			var cap Caption
			for _, c := range afterDoc.Captions {
				if c.ID == op.TargetID {
					cap = c
				}
			}
			if cap.ID == "" {
				return Result{}, errors.New("caption missing")
			}
			var existing, cachedRaw string
			err = tx.QueryRow(ctx, `SELECT status,result::text FROM stitch_alignment_jobs WHERE project_id=$1 AND alignment_key=$2`, project, cap.AlignmentKey).Scan(&existing, &cachedRaw)
			if errors.Is(err, pgx.ErrNoRows) {
				if _, err = tx.Exec(ctx, `INSERT INTO stitch_alignment_jobs(project_id,owner_id,caption_id,language,model_version,source_video_id,source_start_us,source_end_us,start_us,end_us,text,alignment_key,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'pending')`, project, owner, cap.ID, op.Language, op.ModelVersion, cap.SourceVideoID, cap.SourceStartUS, cap.SourceEndUS, cap.StartUS, cap.EndUS, cap.Text, cap.AlignmentKey); err != nil {
					return Result{}, err
				}
				if _, err = tx.Exec(ctx, `SELECT pg_notify('stitch_alignment_jobs_changed',$1)`, uuidText(project)); err != nil {
					return Result{}, err
				}
			} else if err != nil {
				return Result{}, err
			} else if existing == "completed" {
				var cached alignmentResult
				if json.Unmarshal([]byte(cachedRaw), &cached) == nil && cached.State != "" {
					for i := range afterDoc.Captions {
						if afterDoc.Captions[i].ID != cap.ID {
							continue
						}
						afterDoc.Captions[i].Alignment = cached.State
						if cached.State == "valid" {
							afterDoc.Captions[i].Words = nil
							for _, w := range cached.Words {
								if w.Start != nil && w.End != nil && *w.End > *w.Start {
									afterDoc.Captions[i].Words = append(afterDoc.Captions[i].Words, Word{Text: w.Text, StartUS: cap.StartUS + int64(*w.Start*1e6), EndUS: cap.StartUS + int64(*w.End*1e6)})
								}
							}
						}
					}
				}
			}
		}
	}
	afterDoc, err = validateSources(ctx, tx, owner, afterDoc)
	if err != nil {
		return Result{}, err
	}
	after, _ := json.Marshal(afterDoc)
	changedJSON, _ := json.Marshal(changed)
	var id pgtype.UUID
	err = tx.QueryRow(ctx, `INSERT INTO stitch_edits(project_id,owner_id,operation_key,request_hash,revision,actor_kind,actor_id,actor_name,summary,kind,before_document,after_document,changed_ids,operations) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, project, owner, key, hash, snap.Revision+1, actor.Kind, actor.ID, actor.Name, summary, kind, before, after, changedJSON, opJSON).Scan(&id)
	if err != nil {
		return Result{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE stitch_projects SET document=$3, revision=$4, undo_stack=array_append(undo_stack,$5), redo_stack='{}', title=$6, updated_at=now() WHERE id=$1 AND created_by=$2`, project, owner, after, snap.Revision+1, id, afterDoc.Title)
	if err != nil {
		return Result{}, err
	}
	_, err = tx.Exec(ctx, `SELECT pg_notify('stitch_projects_changed',$1)`, uuidText(project))
	if err != nil {
		return Result{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{Snapshot: Snapshot{ID: project, Revision: snap.Revision + 1, Enabled: true, Document: afterDoc, Description: snap.Description, Tags: snap.Tags}, ChangedIDs: changed, EditID: id, Summary: summary}, nil
}

// Commit applies a bounded operation batch atomically at the expected revision.
func (s *Store) Commit(ctx context.Context, ownerID, projectID pgtype.UUID, expectedRevision int64, key string, actor Actor, summary string, ops []Operation) (Result, error) {
	return s.commit(ctx, ownerID, projectID, expectedRevision, key, actor, summary, "edit", ops)
}

// History returns immutable edit events after a revision, oldest first.
func (s *Store) History(ctx context.Context, ownerID, projectID pgtype.UUID, afterRevision int64, limit int) ([]Event, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stitch_projects WHERE id=$1)`, projectID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(ctx, `SELECT e.id,e.revision,e.actor_kind,e.actor_id,e.actor_name,e.summary,e.kind,e.changed_ids,e.operations,e.created_at FROM stitch_edits e WHERE e.project_id=$1 AND e.revision>$2 ORDER BY e.revision LIMIT $3`, projectID, afterRevision, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var ids, ops []byte
		if err := rows.Scan(&e.ID, &e.Revision, &e.Actor.Kind, &e.Actor.ID, &e.Actor.Name, &e.Summary, &e.Kind, &ids, &ops, &e.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(ids, &e.ChangedIDs)
		_ = json.Unmarshal(ops, &e.Operations)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Undo records a new revision restoring the latest undoable edit's prior snapshot.
func (s *Store) Undo(ctx context.Context, ownerID, projectID pgtype.UUID, expectedRevision int64, key string, actor Actor, summary string) (Result, error) {
	return s.historyMove(ctx, ownerID, projectID, expectedRevision, key, actor, summary, false)
}

// Redo records a new revision restoring the latest undone edit's after snapshot.
func (s *Store) Redo(ctx context.Context, ownerID, projectID pgtype.UUID, expectedRevision int64, key string, actor Actor, summary string) (Result, error) {
	return s.historyMove(ctx, ownerID, projectID, expectedRevision, key, actor, summary, true)
}

func (s *Store) historyMove(ctx context.Context, owner, project pgtype.UUID, expected int64, key string, actor Actor, summary string, redo bool) (Result, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)
	snap, undo, red, err := s.snapshot(ctx, tx, owner, project, true)
	if err != nil {
		return Result{}, err
	}
	hash, _, _ := hashRequest(struct {
		Kind     string `json:"kind"`
		Expected int64  `json:"expected_revision"`
		Summary  string `json:"summary"`
	}{"redo", expected, summary})
	if !redo {
		hash, _, _ = hashRequest(struct {
			Kind     string `json:"kind"`
			Expected int64  `json:"expected_revision"`
			Summary  string `json:"summary"`
		}{"undo", expected, summary})
	}
	opJSON := []byte("[]")
	var existing pgtype.UUID
	var exHash string
	var exRev int64
	var exRaw, exIDs []byte
	err = tx.QueryRow(ctx, `SELECT e.id,e.request_hash,e.revision,e.after_document,e.changed_ids FROM stitch_edits e JOIN stitch_projects p ON p.id=e.project_id WHERE e.project_id=$1 AND p.created_by=$2 AND e.operation_key=$3`, project, owner, key).Scan(&existing, &exHash, &exRev, &exRaw, &exIDs)
	if err == nil {
		if exHash != hash {
			return Result{}, ErrIdempotency
		}
		d, e := decodeDocument(exRaw)
		if e != nil {
			return Result{}, e
		}
		var ids []string
		_ = json.Unmarshal(exIDs, &ids)
		return Result{Snapshot: Snapshot{ID: project, Revision: exRev, Enabled: true, Document: d, Description: snap.Description, Tags: snap.Tags}, ChangedIDs: ids, EditID: existing, Summary: summary}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, err
	}
	if !snap.Enabled {
		return Result{}, ErrNotFound
	}
	if snap.Revision != expected {
		return Result{}, &ConflictError{CurrentRevision: snap.Revision}
	}
	if len(undo) == 0 && !redo {
		return Result{}, fmt.Errorf("no undo history")
	}
	if len(red) == 0 && redo {
		return Result{}, fmt.Errorf("no redo history")
	}
	var target pgtype.UUID
	if redo {
		target = red[len(red)-1]
	} else {
		target = undo[len(undo)-1]
	}
	var before, after, ids []byte
	if err = tx.QueryRow(ctx, `SELECT before_document,after_document,changed_ids FROM stitch_edits WHERE id=$1 AND project_id=$2`, target, project).Scan(&before, &after, &ids); err != nil {
		return Result{}, err
	}
	desired := before
	if redo {
		desired = after
	}
	var d Document
	if d, err = decodeDocument(desired); err != nil {
		return Result{}, err
	}
	old, _ := json.Marshal(snap.Document)
	rev := snap.Revision + 1
	kind := "undo"
	if redo {
		kind = "redo"
	}
	var id pgtype.UUID
	if err = tx.QueryRow(ctx, `INSERT INTO stitch_edits(project_id,owner_id,operation_key,request_hash,revision,actor_kind,actor_id,actor_name,summary,kind,before_document,after_document,changed_ids,operations) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, project, owner, key, hash, rev, actor.Kind, actor.ID, actor.Name, summary, kind, old, desired, ids, opJSON).Scan(&id); err != nil {
		return Result{}, err
	}
	if redo {
		red = red[:len(red)-1]
		undo = append(undo, target)
	} else {
		undo = undo[:len(undo)-1]
		red = append(red, target)
	}
	if _, err = tx.Exec(ctx, `UPDATE stitch_projects SET document=$3,revision=$4,undo_stack=$5,redo_stack=$6,updated_at=now() WHERE id=$1 AND created_by=$2`, project, owner, desired, rev, undo, red); err != nil {
		return Result{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_notify('stitch_projects_changed',$1)`, uuidText(project)); err != nil {
		return Result{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	var changed []string
	_ = json.Unmarshal(ids, &changed)
	return Result{Snapshot: Snapshot{ID: project, Revision: rev, Enabled: true, Document: d, Description: snap.Description, Tags: snap.Tags}, ChangedIDs: changed, EditID: id, Summary: summary}, nil
}
