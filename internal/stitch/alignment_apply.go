package stitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
)

type alignmentWordResult struct {
	Text  string   `json:"text"`
	Start *float64 `json:"start"`
	End   *float64 `json:"end"`
}
type alignmentResult struct {
	State string                `json:"state"`
	Words []alignmentWordResult `json:"words"`
	Error string                `json:"error"`
}

// ApplyAlignmentResult persists a runtime result and applies it only to its unchanged caption.
func (s *Store) ApplyAlignmentResult(ctx context.Context, claim AlignmentClaim, raw json.RawMessage) (Result, error) {
	for attempt := 0; attempt < 3; attempt++ {
		r, err := s.applyAlignmentResultOnce(ctx, claim, raw)
		if !errors.Is(err, ErrConflict) {
			return r, err
		}
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
	}
	return Result{}, ErrConflict
}

func (s *Store) applyAlignmentResultOnce(ctx context.Context, claim AlignmentClaim, raw json.RawMessage) (Result, error) {
	var rr alignmentResult
	if e := json.Unmarshal(raw, &rr); e != nil {
		return Result{}, e
	}
	owner, e := parseUUID(claim.OwnerID)
	if e != nil {
		return Result{}, e
	}
	project, e := parseUUID(claim.ProjectID)
	if e != nil {
		return Result{}, e
	}
	snap, e := s.Get(ctx, owner, project)
	if e != nil {
		return Result{}, e
	}
	var eid pgtype.UUID
	var erev int64
	var eraw, echanged []byte
	if e = s.db.QueryRow(ctx, `SELECT e.id,e.revision,e.after_document,e.changed_ids FROM stitch_edits e WHERE e.project_id=$1 AND e.operation_key=$2`, project, "alignment:"+claim.ID.String()).Scan(&eid, &erev, &eraw, &echanged); e == nil {
		doc, e2 := decodeDocument(eraw)
		if e2 != nil {
			return Result{}, e2
		}
		var ids []string
		_ = json.Unmarshal(echanged, &ids)
		return Result{Snapshot: Snapshot{ID: project, Revision: erev, Enabled: true, Document: doc}, ChangedIDs: ids, EditID: eid}, nil
	}
	var c *Caption
	for i := range snap.Document.Captions {
		if snap.Document.Captions[i].ID == claim.CaptionID {
			c = &snap.Document.Captions[i]
		}
	}
	if c == nil {
		_, _ = s.db.Exec(ctx, `UPDATE stitch_alignment_jobs SET status='stale',result=$2,error='caption missing' WHERE id=$1`, claim.ID, raw)
		return Result{}, errors.New("caption missing")
	}
	if c.AlignmentKey != claim.AlignmentKey || c.Text != claim.Text || c.StartUS != claim.StartUS || c.EndUS != claim.EndUS || c.SourceVideoID != claim.SourceVideoID || c.SourceStartUS != claim.SourceStartUS || c.SourceEndUS != claim.SourceEndUS {
		_, _ = s.db.Exec(ctx, `UPDATE stitch_alignment_jobs SET status='stale',result=$2,error='caption changed' WHERE id=$1`, claim.ID, raw)
		return Result{}, fmt.Errorf("stale alignment result")
	}
	if _, e = s.db.Exec(ctx, `UPDATE stitch_alignment_jobs SET result=$2 WHERE id=$1`, claim.ID, raw); e != nil {
		return Result{}, e
	}
	if rr.State != "valid" {
		cap := *c
		cap.Alignment = rr.State
		cap.Words = nil
		if _, e = s.Commit(ctx, owner, project, snap.Revision, "alignment:"+claim.ID.String(), Actor{Kind: "alignment", ID: claim.ID.String()}, "Store caption alignment result", []Operation{{Type: "upsert_caption", Caption: &cap}}); e != nil {
			return Result{}, e
		}
		_, e = s.db.Exec(ctx, `UPDATE stitch_alignment_jobs SET status=$2,error=$3 WHERE id=$1`, claim.ID, rr.State, rr.Error)
		return Result{}, e
	}
	if len(rr.Words) == 0 {
		return Result{}, errors.New("valid alignment has no words")
	}
	cap := *c
	cap.Words = nil
	for _, w := range rr.Words {
		if w.Start == nil || w.End == nil || *w.End <= *w.Start {
			return Result{}, errors.New("invalid alignment word")
		}
		ws := cap.StartUS + int64(*w.Start*1e6)
		we := cap.StartUS + int64(*w.End*1e6)
		if ws < cap.StartUS || we > cap.EndUS {
			return Result{}, errors.New("alignment word out of bounds")
		}
		cap.Words = append(cap.Words, Word{Text: w.Text, StartUS: ws, EndUS: we})
	}
	cap.Alignment = "valid"
	r, e := s.Commit(ctx, owner, project, snap.Revision, "alignment:"+claim.ID.String(), Actor{Kind: "alignment", ID: claim.ID.String()}, "Apply caption alignment", []Operation{{Type: "upsert_caption", Caption: &cap}})
	if e != nil {
		return Result{}, e
	}
	_, e = s.db.Exec(ctx, `UPDATE stitch_alignment_jobs SET status='completed',updated_at=now() WHERE id=$1`, claim.ID)
	return r, e
}
