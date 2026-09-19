//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/contextwindow"
	"thirdcoast.systems/rewind/internal/db"
)

func TestGuidedGenerationRetry(t *testing.T) {
	ctx := context.Background()
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbc := &db.DatabaseConnection{Pool: pool}
	if err := dbc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := dbc.Queries(ctx).WithTx(tx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	user := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	video := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	exec("INSERT INTO users(id,user_name,email,password) VALUES($1,$2,$2,'fixture')", user, user.String())
	exec("INSERT INTO videos(id,src,archived_by,title,media,video_path,duration_seconds) VALUES($1,$2,$3,'repair fixture','video','/fixture.mp4',120)", video, video.String(), user)
	exec(`INSERT INTO video_transcripts(video_id,lang,format,text,raw,cues) VALUES($1,'en','vtt','broken lyrics','original raw','[{"start":0,"end":120,"text":"broken lyrics"}]')`, video)
	a, err := contextwindow.EnqueueRetry(ctx, q, video, "Focus on the discussion.")
	if err != nil {
		t.Fatal(err)
	}
	b, err := contextwindow.EnqueueRetry(ctx, q, video, "Focus on the discussion.")
	if err != nil {
		t.Fatal(err)
	}
	if a.PromptVersion == b.PromptVersion || a.RetryInstructions != "Focus on the discussion." {
		t.Fatal("retry reused identity or lost guidance")
	}
	old, err := q.GetVideoTranscript(ctx, video)
	if err != nil {
		t.Fatal(err)
	}
	start, end := 0.0, 120.0
	job, err := q.EnqueueGenerationRetry(ctx, &db.EnqueueGenerationRetryParams{VideoID: video, Kind: "transcribe", TranscriptHash: a.TranscriptHash, PromptVersion: "repair-test", RangeStart: &start, RangeEnd: &end, RepairTranscript: true})
	if err != nil {
		t.Fatal(err)
	}
	if published, err := q.TranscriptRepairPublished(ctx, job.ID); err != nil || published {
		t.Fatalf("unpublished repair: %v %v", published, err)
	}
	if err := q.BackupTranscriptRepair(ctx, &db.BackupTranscriptRepairParams{JobID: job.ID, VideoID: video, Lang: old.Lang}); err != nil {
		t.Fatal(err)
	}
	n, err := q.ReplaceTranscriptCues(ctx, &db.ReplaceTranscriptCuesParams{VideoID: video, Lang: old.Lang, Text: "Actual discussion", Cues: []byte(`[{"start":0,"end":120,"text":"Actual discussion"}]`)})
	if err != nil || n != 1 {
		t.Fatalf("replace: %d %v", n, err)
	}
	var backup []byte
	if err := tx.QueryRow(ctx, "SELECT transcript FROM transcript_repair_backups WHERE job_id=$1", job.ID).Scan(&backup); err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(backup, &saved); err != nil || saved["text"] != "broken lyrics" || saved["raw"] != "original raw" {
		t.Fatalf("backup lost transcript: %s %v", backup, err)
	}
	current, err := q.GetVideoTranscript(ctx, video)
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "Actual discussion" || len(current.Coverage) != 0 {
		t.Fatal("repair changed coverage or did not publish")
	}
	if published, err := q.TranscriptRepairPublished(ctx, job.ID); err != nil || !published {
		t.Fatalf("published repair cannot be recognized on replay: %v %v", published, err)
	}
	repaired, err := q.GetRepairedVideoTranscript(ctx, video)
	if err != nil || repaired.Text != "Actual discussion" {
		t.Fatalf("caption playback did not select repaired transcript: %v", err)
	}
	if _, err := contextwindow.EnqueueRetry(ctx, q, video, "After audio repair"); err != nil {
		t.Fatal(err)
	}
}
