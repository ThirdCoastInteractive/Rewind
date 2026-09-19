//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"testing"
	"thirdcoast.systems/rewind/internal/stitch"
)

func TestStitchLegacyMultiClipTimingAndMetadata(t *testing.T) {
	d, u, p := openStitchTest(t)
	ctx := context.Background()
	v1 := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	v2 := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	c1 := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	c2 := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	for _, v := range []pgtype.UUID{v1, v2} {
		if _, e := d.Exec(ctx, `INSERT INTO videos(id,src,archived_by,title) VALUES($1,$2,$3,'legacy')`, v, v.String(), u); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := d.Exec(ctx, `INSERT INTO clips(id,video_id,start_ts,end_ts,duration,created_by,title,filter_stack,crops) VALUES($1,$2,100,110,10,$3,'one','[{"kind":"look","value":"warm"}]','{"x":1}'),($4,$5,200,205,5,$3,'two','[{"kind":"look","value":"cool"}]','{"x":2}')`, c1, v1, u, c2, v2); e != nil {
		t.Fatal(e)
	}
	legacy := []byte(`[{"id":"title","type":"title","duration":2,"text":"intro","font":"serif"},{"id":"one","type":"clip","clip_id":"` + c1.String() + `","start_ts":101,"end_ts":109,"duration":99,"gain_db":-2,"look":"warm"},{"id":"two","type":"clip","clip_id":"` + c2.String() + `","start_ts":201,"end_ts":204,"transition":{"type":"fade","duration":1,"extension":"keep"},"font":"sans"}]`)
	if _, e := d.Exec(ctx, `UPDATE stitch_projects SET segments=$1,global_filters='[{"kind":"crop","x":1}]' WHERE id=$2`, legacy, p); e != nil {
		t.Fatal(e)
	}
	s := stitch.NewStore(d)
	snap, e := s.Enable(ctx, u, p)
	if e != nil {
		t.Fatal(e)
	}
	if len(snap.Document.Segments) != 3 {
		t.Fatalf("segments=%d", len(snap.Document.Segments))
	}
	a, b := snap.Document.Segments[1], snap.Document.Segments[2]
	if a.StartUS != 2_000_000 || a.SourceInUS != 101_000_000 || a.DurationUS != 8_000_000 || b.StartUS != 9_000_000 || b.SourceInUS != 201_000_000 || b.DurationUS != 3_000_000 {
		t.Fatalf("timing: %+v %+v", a, b)
	}
	if a.Legacy == nil || b.Legacy == nil || b.Transition == nil || b.Transition.Kind != "fade" || b.Transition.DurationUS != 1_000_000 {
		t.Fatal("legacy metadata lost")
	}
	var bm map[string]any
	if json.Unmarshal(b.Legacy, &bm) != nil || bm["font"] != "sans" {
		t.Fatalf("metadata=%v", bm)
	}
	var am map[string]any
	if json.Unmarshal(a.Legacy, &am) != nil || am["gain_db"] != float64(-2) || am["look"] != "warm" {
		t.Fatalf("clip metadata=%v", am)
	}
	if len(snap.Document.Settings.Filter) == 0 {
		t.Fatal("global filters lost")
	}
	var stored []byte
	if e = d.QueryRow(ctx, `SELECT legacy_snapshot FROM stitch_projects WHERE id=$1`, p).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	var frozen map[string]any
	if e = json.Unmarshal(stored, &frozen); e != nil || frozen["title"] != "Untitled" {
		t.Fatalf("snapshot=%s %v", stored, e)
	}
}
