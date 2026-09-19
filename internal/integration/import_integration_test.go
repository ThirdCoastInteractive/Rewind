//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"testing"
	"thirdcoast.systems/rewind/internal/stitch"
)

func TestStitchCaptionImportReplay(t *testing.T) {
	d, u, p := openStitchTest(t)
	ctx := context.Background()
	v := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, e := d.Exec(ctx, `INSERT INTO videos(id,src,archived_by,title) VALUES($1,$3,$2,'source')`, v, u, "fixture-"+v.String()); e != nil {
		t.Fatal(e)
	}
	raw := `[{"start":1,"end":2,"text":"hello"}]`
	if _, e := d.Exec(ctx, `INSERT INTO video_transcripts(video_id,lang,format,text,raw,cues) VALUES($1,'en','vtt','hello','', $2)`, v, []byte(raw)); e != nil {
		t.Fatal(e)
	}
	s := stitch.NewStore(d)
	if _, e := s.Enable(ctx, u, p); e != nil {
		t.Fatal(e)
	}
	a := stitch.Actor{Kind: "user", ID: u.String()}
	r, e := s.Commit(ctx, u, p, 0, "insert", a, "insert", []stitch.Operation{{Type: "insert_segment", Segment: &stitch.Segment{ID: "seg", Type: "video", VideoID: v.String(), DurationUS: 5e6}}})
	if e != nil {
		t.Fatal(e)
	}
	originalExpected := r.Revision
	r, e = s.ImportCaptions(ctx, u, p, originalExpected, "import", a, "seg", "en")
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Document.Captions) != 1 {
		t.Fatalf("captions: %d", len(r.Document.Captions))
	}
	_, e = s.Commit(ctx, u, p, r.Revision, "edit", a, "edit", []stitch.Operation{{Type: "set_title", Title: "edited"}})
	if e != nil {
		t.Fatal(e)
	}
	r2, e := s.ImportCaptions(ctx, u, p, originalExpected, "import", a, "seg", "en")
	if e != nil {
		t.Fatal(e)
	}
	if r2.Revision != r.Revision {
		t.Fatalf("replay revision %d", r2.Revision)
	}
	if _, e = s.ImportCaptions(ctx, u, p, r.Revision, "import-different", a, "seg", "en"); !errors.Is(e, stitch.ErrConflict) && !errors.Is(e, stitch.ErrIdempotency) {
		t.Fatalf("expected stale or idempotency error: %v", e)
	}
	b, _ := json.Marshal(r.Document)
	_ = b
}
