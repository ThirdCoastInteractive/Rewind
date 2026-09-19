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

func TestStitchCompilationCanonicalInitializationIsIdempotent(t *testing.T) {
	d, owner, project := openStitchTest(t)
	ctx := context.Background()
	video := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := d.Exec(ctx, `INSERT INTO videos(id,src,archived_by,title) VALUES($1,$2,$3,'fixture')`, video, "compilation-source-"+video.String(), owner); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal([]map[string]any{{"type": "video", "video_id": video.String(), "start_ts": 0, "end_ts": 2, "duration": 2}})
	tx, err := d.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stitch.InitializeFromLegacy(ctx, tx, owner, project, "Compilation", "mp4", "high", payload, []byte("[]")); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var legacyTitle string
	var legacySegments []byte
	if err = d.QueryRow(ctx, `SELECT title,segments FROM stitch_projects WHERE id=$1`, project).Scan(&legacyTitle, &legacySegments); err != nil {
		t.Fatal(err)
	}
	if legacyTitle != "Compilation" || len(legacySegments) == 0 || string(legacySegments) == "[]" {
		t.Fatalf("legacy fields were not preserved: %q %s", legacyTitle, legacySegments)
	}
	if _, err = d.Exec(ctx, `UPDATE stitch_projects SET revision=3 WHERE id=$1`, project); err != nil {
		t.Fatal(err)
	}
	s := stitch.NewStore(d)
	enabled, err := s.Enable(ctx, owner, project)
	if err != nil || enabled.Revision != 3 {
		t.Fatalf("enable changed revision: %d %v", enabled.Revision, err)
	}
	changed, err := s.Commit(ctx, owner, project, 3, "compilation-edit", stitch.Actor{Kind: "user", ID: owner.String()}, "change", []stitch.Operation{{Type: "set_title", Title: "Edited"}})
	if err != nil {
		t.Fatal(err)
	}
	tx, err = d.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, initErr := stitch.InitializeFromLegacy(ctx, tx, owner, project, "Overwrite", "webm", "high", payload, []byte("[]"))
	_ = tx.Rollback(ctx)
	if initErr == nil {
		t.Fatal("canonical project was reinitialized")
	}
	snap, err := s.Get(ctx, owner, project)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Document.Title != "Edited" || snap.Revision != changed.Revision || !snap.Enabled {
		t.Fatalf("canonical state changed after replay: title=%q revision=%d enabled=%v", snap.Document.Title, snap.Revision, snap.Enabled)
	}
}
