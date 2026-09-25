//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/compilation"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/contextwindow"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/vision"
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

func TestDurableWorkflow(t *testing.T) {
	// Deliberately fixed to the disposable Compose service: never read DATABASE_DSN.
	ctx := context.Background()
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbc := &db.DatabaseConnection{Pool: pool}
	if err = dbc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	plugin.Reset()
	t.Cleanup(plugin.Reset)
	builtin.Defaults(dbc)
	q := dbc.Queries(ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func() pgtype.UUID { return pgtype.UUID{Bytes: uuid.New(), Valid: true} }
	user, video := id(), id()
	exec("INSERT INTO users(id,user_name,email,password) VALUES($1,$2,$2,'fixture')", user, user.String())
	exec("INSERT INTO videos(id,src,archived_by,title,media,video_path,duration_seconds) VALUES($1,$2,$3,'fixture','video','/fixture.mp4',120)", video, video.String(), user)
	exec(`INSERT INTO video_transcripts(video_id,lang,format,text,raw,cues) VALUES($1,'en','vtt','hello world','', '[{"start":0,"end":1,"text":"hello world"}]')`, video)
	first, err := q.GetTranscriptFingerprint(ctx, video)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := q.ListMLJobsForVideo(ctx, video)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("transcript import unexpectedly enqueued ML work: %v %d", err, len(jobs))
	}
	if err := q.EnqueueMLJob(ctx, &db.EnqueueMLJobParams{VideoID: video, Kind: "context_windows", Priority: 100, TranscriptHash: first, ModelDigest: "fixture", PromptVersion: contextwindow.PromptVersion}); err != nil {
		t.Fatal("explicit demand enqueue:", err)
	}
	jobs, err = q.ListMLJobsForVideo(ctx, video)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("explicit enqueue: %v %d", err, len(jobs))
	}
	exec(`UPDATE video_transcripts SET cues='[{"start":2,"end":3,"text":"hello world"}]' WHERE video_id=$1`, video)
	second, err := q.GetTranscriptFingerprint(ctx, video)
	if err != nil || first == second {
		t.Fatal("timing fingerprint unchanged", err)
	}
	jobs, err = q.ListMLJobsForVideo(ctx, video)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("timing update unexpectedly enqueued ML work: %v %d", err, len(jobs))
	}
	if err := q.EnqueueMLJob(ctx, &db.EnqueueMLJobParams{VideoID: video, Kind: "context_windows", Priority: 100, TranscriptHash: second, ModelDigest: "fixture", PromptVersion: contextwindow.PromptVersion}); err != nil {
		t.Fatal("explicit refreshed enqueue:", err)
	}
	jobs, err = q.ListMLJobsForVideo(ctx, video)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("explicit refreshed enqueue: %v %d", err, len(jobs))
	}
	exec(`UPDATE ml_jobs SET status='succeeded' WHERE video_id=$1`, video)
	window, err := q.CreateContextWindow(ctx, &db.CreateContextWindowParams{VideoID: video, StartTs: 2, EndTs: 20, Title: "old", Summary: "old", Topics: []string{}, Entities: []string{}, Origin: "mcp", TranscriptCueEvidence: []byte("[]"), BoundaryQuality: "cue", CreatedBy: user})
	if err != nil {
		t.Fatal(err)
	}
	zero, empty := 0.0, ""
	edited, err := contextwindow.Edit(ctx, dbc, window.ID, contextwindow.Patch{Start: &zero, Summary: &empty})
	if err != nil || edited.StartTs != 0 || edited.Summary != "" || !edited.OverrideBounds || !edited.OverrideSummary {
		t.Fatalf("optional edits: %+v %v", edited, err)
	}
	plan, err := q.CreateCompilationPlan(ctx, &db.CreateCompilationPlanParams{CreatedBy: user, Title: "editorial"})
	if err != nil {
		t.Fatal(err)
	}
	for position, start := range []float64{60, 10} {
		_, err = q.AddCompilationPlanSegment(ctx, &db.AddCompilationPlanSegmentParams{PlanID: plan.ID, Position: int32(position), VideoID: video, StartTs: start, EndTs: start + 5, MatchEvidence: []byte("{}")})
		if err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	ids := make(chan pgtype.UUID, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := compilation.Execute(ctx, dbc, plan.ID, user, plan.Revision, false)
			if err != nil {
				errs <- err
			} else {
				ids <- e.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var executionID pgtype.UUID
	for result := range ids {
		if executionID.Valid && executionID != result {
			t.Fatal("duplicate execution")
		}
		executionID = result
	}
	segments, err := q.ListExecutionSegments(ctx, executionID)
	if err != nil || len(segments) != 2 || segments[0].StartTs != 60 || segments[1].StartTs != 10 {
		t.Fatal("editorial order lost", err)
	}
	if _, err = compilation.Advance(ctx, dbc); err != nil {
		t.Fatal(err)
	}
	runs, err := q.ListCompilationExecutions(ctx, plan.ID)
	if err != nil || len(runs) != 1 || runs[0].Status != "rendering" || !runs[0].StitchJobID.Valid {
		t.Fatal("render continuation", err, runs)
	}
	if _, err = compilation.Execute(ctx, dbc, plan.ID, id(), plan.Revision, false); err == nil {
		t.Fatal("owner check bypassed")
	}
	if _, err = q.BumpCompilationPlanRevision(ctx, &db.BumpCompilationPlanRevisionParams{ID: plan.ID}); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE stitch_jobs SET status='ready',file_path='/fixture-output.mp4' WHERE id=$1", runs[0].StitchJobID)
	exec("UPDATE compilation_executions SET next_check=now() WHERE id=$1", executionID)
	if _, err = compilation.Advance(ctx, dbc); err != nil {
		t.Fatal(err)
	}
	newer, err := q.GetCompilationPlan(ctx, plan.ID)
	if err != nil || newer.Status != "draft" || newer.StitchJobID.Valid {
		t.Fatal("old revision overwrote new plan", err, newer)
	}
	vector, err := vision.Normalize("[1," + strings.Repeat("0,", 510) + "0]")
	if err != nil {
		t.Fatal(err)
	}
	modelID := uuid.NewString()
	exec("INSERT INTO embedding_models(id,name,revision,recipe,dimensions,license) VALUES($1,'fixture','1','1',512,'test')", modelID)
	setID := id()
	exec("INSERT INTO visual_index_sets(id,video_id,model_id,asset_fingerprint,end_ts,status,active,next_sample) VALUES($1,$2,$3,'fixture',120,'succeeded',true,24)", setID, video, modelID)
	exec("INSERT INTO visual_frame_embeddings(set_id,sample_index,sample_ts,frame_ref,embedding) VALUES($1,0,0,'fixture',$2::vector)", setID, vector)
	hits, err := q.SearchVisualEmbeddings(ctx, &db.SearchVisualEmbeddingsParams{ModelID: modelID, Embedding: vector, VideoID: video, CandidateLimit: 20})
	if err != nil || len(hits) != 1 || hits[0].Similarity < .999 {
		t.Fatal("vector query", err, fmt.Sprint(hits))
	}
	if err = contextwindow.Enqueue(ctx, dbc, video); err != nil {
		t.Fatal(err)
	}
	jobs, err = q.ListMLJobsForVideo(ctx, video)
	if err != nil || len(jobs) == 0 {
		t.Fatal("enqueue current transcript", err)
	}
	exec("UPDATE ml_jobs SET status='failed' WHERE video_id=$1 AND kind='context_windows'", video)
	n, err := q.RetryMLJob(ctx, jobs[0].ID)
	if err != nil || n == 0 {
		t.Fatal("operator retry", err, n)
	}
	if err = q.WakePendingCompilations(ctx); err != nil {
		t.Fatal(err)
	}
	// The retired pipeline cannot be re-enqueued by an old service.
	if _, err := pool.Exec(ctx, "INSERT INTO ml_jobs(video_id,kind,transcript_hash) VALUES($1,'face_index','retirement-test')", video); err == nil || !strings.Contains(err.Error(), "Face identification has been removed") {
		t.Fatalf("retired face job accepted: %v", err)
	}
	// Completed historical rows remain available, but cannot be restarted.
	exec("INSERT INTO ml_jobs(video_id,kind,status,transcript_hash) VALUES($1,'face_index','succeeded','retirement-test')", video)
	if _, err := pool.Exec(ctx, "UPDATE ml_jobs SET status='queued' WHERE video_id=$1 AND kind='face_index'", video); err == nil {
		t.Fatal("historical face job restarted")
	}

}
