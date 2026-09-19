package stitch

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// TeaserCreateInput describes the immutable inputs used to create a teaser.
// The operation key is scoped to the owner and makes retries safe.
type TeaserCreateInput struct {
	Owner        pgtype.UUID
	OperationKey string
	Title        string
	Width        int
	Height       int
	Segments     []Segment
	Actor        Actor
}

// TeaserCreateResult is the canonical project snapshot returned by creation.
type TeaserCreateResult struct {
	Snapshot     Snapshot `json:"snapshot"`
	OperationKey string   `json:"operation_key"`
	Created      bool     `json:"created"`
}

// CreateTeaser atomically creates a canonical Stitch project and its initial
// immutable edit. Repeating the same owner and operation key returns the same
// project when the request is unchanged, while a changed request is rejected.
func (s *Store) CreateTeaser(ctx context.Context, in TeaserCreateInput) (TeaserCreateResult, error) {
	key := strings.TrimSpace(in.OperationKey)
	if !in.Owner.Valid {
		return TeaserCreateResult{}, errors.New("owner is required")
	}
	if len(key) < 1 || len(key) > 200 {
		return TeaserCreateResult{}, errors.New("operation_key must be 1-200 characters")
	}
	if in.Width == 0 {
		in.Width = 1080
	}
	if in.Height == 0 {
		in.Height = 1920
	}
	if in.Width <= 0 || in.Height <= 0 || in.Width > 16384 || in.Height > 16384 || in.Width%2 != 0 || in.Height%2 != 0 {
		return TeaserCreateResult{}, errors.New("width and height must be positive even values no larger than 16384")
	}
	if len(in.Segments) == 0 || len(in.Segments) > 100 {
		return TeaserCreateResult{}, errors.New("segments must contain 1-100 items")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Untitled teaser"
	}
	for i := range in.Segments {
		if in.Segments[i].ID == "" {
			in.Segments[i].ID = fmt.Sprintf("teaser-segment-%d", i+1)
		}
		if in.Segments[i].Type == "" {
			in.Segments[i].Type = "video"
		}
		if in.Segments[i].Type != "video" && in.Segments[i].Type != "clip" {
			return TeaserCreateResult{}, fmt.Errorf("segment %q must be video or clip", in.Segments[i].ID)
		}
	}
	d := Document{Version: CurrentVersion, Title: title, FPS: 30, Width: in.Width, Height: in.Height, Segments: in.Segments, Captions: []Caption{}, Overlays: []Overlay{}, TimingLinks: []Group{}, PositionGroups: []Group{}, Settings: Settings{Format: "mp4", Quality: "high", CaptionMode: "burn"}}
	for i := range d.Segments {
		if d.Segments[i].Layout == nil {
			d.Segments[i].Layout = DefaultTeaserLayout()
		}
	}
	if err := Validate(d); err != nil {
		return TeaserCreateResult{}, fmt.Errorf("invalid teaser document: %w", err)
	}
	hash, _, err := hashRequest(struct {
		Title    string    `json:"title"`
		Width    int       `json:"width"`
		Height   int       `json:"height"`
		Segments []Segment `json:"segments"`
	}{title, in.Width, in.Height, in.Segments})
	if err != nil {
		return TeaserCreateResult{}, err
	}
	project := teaserProjectID(in.Owner, key)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TeaserCreateResult{}, err
	}
	defer tx.Rollback(ctx)
	// The deterministic project ID does not provide a row lock on the first
	// request. Serialize that case so two identical retries cannot both insert.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, in.Owner.String()+"|teaser|"+key); err != nil {
		return TeaserCreateResult{}, err
	}
	var existingOwner pgtype.UUID
	var existingDoc []byte
	var existingRevision int64
	err = tx.QueryRow(ctx, `SELECT created_by, document, revision FROM stitch_projects WHERE id=$1 FOR UPDATE`, project).Scan(&existingOwner, &existingDoc, &existingRevision)
	if err == nil {
		if existingOwner != in.Owner {
			return TeaserCreateResult{}, ErrNotFound
		}
		var editID pgtype.UUID
		var oldHash string
		var revision int64
		var after []byte
		err = tx.QueryRow(ctx, `SELECT id,request_hash,revision,after_document FROM stitch_edits WHERE project_id=$1 AND operation_key=$2`, project, key).Scan(&editID, &oldHash, &revision, &after)
		if errors.Is(err, pgx.ErrNoRows) {
			return TeaserCreateResult{}, ErrIdempotency
		}
		if err != nil {
			return TeaserCreateResult{}, err
		}
		if oldHash != hash {
			return TeaserCreateResult{}, ErrIdempotency
		}
		var existing Document
		if err := json.Unmarshal(existingDoc, &existing); err != nil {
			return TeaserCreateResult{}, err
		}
		return TeaserCreateResult{Snapshot: Snapshot{ID: project, Revision: existingRevision, Enabled: true, Document: existing}, OperationKey: key}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return TeaserCreateResult{}, err
	}
	if err = validateTeaserSources(ctx, tx, d); err != nil {
		return TeaserCreateResult{}, err
	}
	doc, err := json.Marshal(d)
	if err != nil {
		return TeaserCreateResult{}, err
	}
	initial := Document{Version: CurrentVersion, Title: title, FPS: 30, Width: in.Width, Height: in.Height, Settings: d.Settings}
	before, err := json.Marshal(initial)
	if err != nil {
		return TeaserCreateResult{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO stitch_projects (id,created_by,title,format,quality,segments,global_filters,document,document_version,revision,editor_enabled,undo_stack,redo_stack) VALUES ($1,$2,$3,'mp4','high','[]','[]',$4,1,1,true,'{}','{}')`, project, in.Owner, title, doc); err != nil {
		return TeaserCreateResult{}, err
	}
	var editID pgtype.UUID
	if err = tx.QueryRow(ctx, `INSERT INTO stitch_edits (project_id,owner_id,operation_key,request_hash,revision,actor_kind,actor_id,actor_name,summary,kind,before_document,after_document,changed_ids,operations) VALUES ($1,$2,$3,$4,1,$5,$6,$7,$8,'create',$9,$10,'[]','[]') RETURNING id`, project, in.Owner, key, hash, actorKind(in.Actor), actorID(in.Actor), in.Actor.Name, "create teaser", before, doc).Scan(&editID); err != nil {
		return TeaserCreateResult{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE stitch_projects SET undo_stack=array_append(undo_stack,$2), updated_at=now() WHERE id=$1`, project, editID); err != nil {
		return TeaserCreateResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return TeaserCreateResult{}, err
	}
	return TeaserCreateResult{Snapshot: Snapshot{ID: project, Revision: 1, Enabled: true, Document: d}, OperationKey: key, Created: true}, nil
}

func validateTeaserSources(ctx context.Context, tx pgx.Tx, d Document) error {
	for _, s := range d.Segments {
		if s.VideoID == "" {
			continue
		}
		id, err := parseUUID(s.VideoID)
		if err != nil {
			return fmt.Errorf("segment %q: invalid video id: %w", s.ID, err)
		}
		var duration *float64
		if err := tx.QueryRow(ctx, `SELECT duration_seconds FROM videos WHERE id=$1`, id).Scan(&duration); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("segment %q: video %s is unavailable", s.ID, s.VideoID)
			}
			return err
		}
		if duration != nil && *duration > 0 && float64(s.SourceInUS+s.DurationUS)/1e6 > *duration+0.001 {
			return fmt.Errorf("segment %q exceeds source video duration", s.ID)
		}
	}
	return nil
}

func actorKind(a Actor) string {
	if strings.TrimSpace(a.Kind) == "" {
		return "agent"
	}
	return string(a.Kind)
}

func actorID(a Actor) string {
	if strings.TrimSpace(a.ID) == "" {
		return "agent"
	}
	return a.ID
}

func teaserProjectID(owner pgtype.UUID, key string) pgtype.UUID {
	h := sha256.Sum256(append(append([]byte{}, owner.Bytes[:]...), []byte("|teaser|")...))
	h = sha256.Sum256(append(h[:], []byte(key)...))
	var b [16]byte
	copy(b[:], h[:16])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return pgtype.UUID{Bytes: b, Valid: true}
}
