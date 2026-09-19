//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/stitch"
)

func TestStitchAlignmentClaimApplyAndStale(t *testing.T) {
	d, u, p := openStitchTest(t)
	ctx := context.Background()
	v := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := d.Exec(ctx, `INSERT INTO videos(id,src,archived_by,title) VALUES($1,$2,$3,'alignment source')`, v, "alignment-"+v.String(), u); err != nil {
		t.Fatal(err)
	}
	s := stitch.NewStore(d)
	if _, err := s.Enable(ctx, u, p); err != nil {
		t.Fatal(err)
	}
	a := stitch.Actor{Kind: "user", ID: u.String()}
	r, err := s.Commit(ctx, u, p, 0, "align-seg", a, "segment", []stitch.Operation{{Type: "insert_segment", Segment: &stitch.Segment{ID: "align-seg", Type: "video", VideoID: v.String(), DurationUS: 3_000_000}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Commit(ctx, u, p, r.Revision, "align-caption", a, "caption", []stitch.Operation{{Type: "upsert_caption", Caption: &stitch.Caption{ID: "align-cap", SegmentID: "align-seg", Text: "hello", StartUS: 0, EndUS: 2_000_000, SourceVideoID: v.String(), SourceStartUS: 0, SourceEndUS: 2_000_000}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Commit(ctx, u, p, r.Revision, "queue-align", a, "queue", []stitch.Operation{{Type: "request_alignment", TargetID: "align-cap", Language: "en", ModelVersion: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := stitch.ClaimAlignment(ctx, d, "integration-worker")
	if err != nil || claim == nil {
		t.Fatalf("claim: %v %+v", err, claim)
	}
	r, err = s.ApplyAlignmentResult(ctx, *claim, json.RawMessage(`{"state":"valid","words":[{"text":"hello","start":0.2,"end":0.8}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Revision != 4 || r.Document.Captions[0].Alignment != "valid" || len(r.Document.Captions[0].Words) != 1 {
		t.Fatalf("apply: rev=%d doc=%+v", r.Revision, r.Document.Captions)
	}
	var state string
	if err := d.QueryRow(ctx, `SELECT status FROM stitch_alignment_jobs WHERE id=$1`, claim.ID).Scan(&state); err != nil || state != "completed" {
		t.Fatalf("completed state: %s %v", state, err)
	}
	// A new editor request with the same immutable fingerprint reuses the
	// completed runtime result in the same commit.
	r, err = s.Commit(ctx, u, p, r.Revision, "queue-align-cache", a, "queue", []stitch.Operation{{Type: "request_alignment", TargetID: "align-cap", Language: "en", ModelVersion: "test"}})
	if err != nil || r.Document.Captions[0].Alignment != "valid" || len(r.Document.Captions[0].Words) != 1 {
		t.Fatalf("cached result: rev=%d cap=%+v err=%v", r.Revision, r.Document.Captions[0], err)
	}
	// Edit then queue another job, then edit again. The old claim must become stale.
	r, err = s.Commit(ctx, u, p, r.Revision, "edit-caption", a, "edit", []stitch.Operation{{Type: "upsert_caption", Caption: &stitch.Caption{ID: "align-cap", SegmentID: "align-seg", Text: "changed", StartUS: 0, EndUS: 2_000_000, SourceVideoID: v.String(), SourceStartUS: 0, SourceEndUS: 2_000_000}}})
	if err != nil {
		t.Fatal(err)
	}
	// The same immutable request fingerprint reuses the existing job row.
	r, err = s.Commit(ctx, u, p, r.Revision, "queue-align-2-repeat", a, "queue", []stitch.Operation{{Type: "request_alignment", TargetID: "align-cap", Language: "en", ModelVersion: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := d.QueryRow(ctx, `SELECT count(*) FROM stitch_alignment_jobs WHERE project_id=$1 AND caption_id='align-cap'`, p).Scan(&jobs); err != nil || jobs != 2 {
		t.Fatalf("job reuse count=%d err=%v", jobs, err)
	}
	r, err = s.Commit(ctx, u, p, r.Revision, "queue-align-2", a, "queue", []stitch.Operation{{Type: "request_alignment", TargetID: "align-cap", Language: "en", ModelVersion: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	claim2, err := stitch.ClaimAlignment(ctx, d, "integration-worker-2")
	if err != nil || claim2 == nil {
		t.Fatalf("claim2: %v %+v", err, claim2)
	}
	r, err = s.Commit(ctx, u, p, r.Revision, "edit-caption-2", a, "edit", []stitch.Operation{{Type: "upsert_caption", Caption: &stitch.Caption{ID: "align-cap", SegmentID: "align-seg", Text: "changed again", StartUS: 0, EndUS: 2_000_000, SourceVideoID: v.String(), SourceStartUS: 0, SourceEndUS: 2_000_000}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyAlignmentResult(ctx, *claim2, json.RawMessage(`{"state":"valid","words":[{"text":"hello","start":0,"end":1}]}`)); err == nil {
		t.Fatal("expected stale result")
	}
	latest, err := s.Get(ctx, u, p)
	if err != nil || latest.Document.Captions[0].Text != "changed again" || latest.Document.Captions[0].Alignment == "valid" {
		t.Fatalf("stale overwrite: %+v %v", latest.Document.Captions[0], err)
	}
}
