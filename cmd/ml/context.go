package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/cmd/ml/internal/mlcore"
	"thirdcoast.systems/rewind/internal/contextwindow"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/modelruntime"
	"thirdcoast.systems/rewind/internal/topics"
	"thirdcoast.systems/rewind/internal/vision"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func enqueueDemandContext(ctx context.Context, dbc *db.DatabaseConnection) error {
	// Paused rows are left paused: an operator clear or agent pause must stick.
	return dbc.Queries(ctx).EnqueueDemandContextJobs(ctx, &db.EnqueueDemandContextJobsParams{
		ModelDigest:   "",
		PromptVersion: mlcore.PromptVersion,
	})
}

func handleContextWindows(ctx context.Context, dbc *db.DatabaseConnection, job *db.MlJob, ollama *mlcore.Ollama, primary string, challengers []string) error {
	q := dbc.Queries(ctx)
	promptVersion := contextwindow.GenerationVersion(job.PromptVersion)
	videoID := job.VideoID
	video, err := q.GetVideoByID(ctx, videoID)
	if err != nil {
		return fmt.Errorf("source video: %w", err)
	}
	if video.TenantID.Valid && video.TenantID.Bytes != [16]byte{} {
		// Hosted workers may run without the Live plugin registered; the source
		// video remains the authoritative workspace scope.
		ctx = plugin.WithTenantScope(ctx, video.TenantID.String(), true)
	} else if plugin.LiveIngest() != nil {
		if !video.TenantID.Valid || video.TenantID.Bytes == [16]byte{} {
			return fmt.Errorf("wiki tenant is required for Live context generation")
		}
	}
	hash, err := q.GetTranscriptFingerprint(ctx, videoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("waiting_assets: transcript fingerprint missing")
		}
		return fmt.Errorf("transcript fingerprint: %w", err)
	}
	if job.TranscriptHash != "" && job.TranscriptHash != hash {
		return mlcore.ErrSuperseded
	}
	capCues, _, err := loadTranscriptCues(ctx, q, videoID)
	if err != nil {
		return fmt.Errorf("transcript: %w", err)
	}
	cues := mlcore.CuesFromCaptions(capCues)
	if len(cues) == 0 {
		return fmt.Errorf("transcript has no cues")
	}

	modelName, digest, release, err := acquireRunnableContextModel(ctx, ollama, primary, challengers)
	if err != nil {
		return err
	}
	defer release()
	job.ModelDigest = digest
	job.PromptVersion = promptVersion
	if existing, gerr := q.GetContextWindowSetByKey(ctx, &db.GetContextWindowSetByKeyParams{
		VideoID: videoID, TranscriptHash: hash, ModelDigest: digest, PromptVersion: promptVersion,
	}); gerr == nil && existing != nil && existing.Status == "succeeded" {
		return mlcore.ErrSuperseded
	} else if gerr != nil && !errors.Is(gerr, pgx.ErrNoRows) {
		return gerr
	}
	set, err := q.CreateContextWindowSet(ctx, &db.CreateContextWindowSetParams{
		VideoID:        videoID,
		TranscriptHash: hash,
		ModelDigest:    digest,
		PromptVersion:  promptVersion,
		Status:         "processing",
		Metrics:        []byte(`{}`),
	})
	if err != nil {
		return fmt.Errorf("create context window set: %w", err)
	}
	if set.Status == "succeeded" {
		return mlcore.ErrSuperseded
	}

	topicStore := topics.New(dbc)
	if err := topicStore.SeedWiki(ctx); err != nil {
		slog.Warn("topic wiki seed", "error", err)
	}
	catalog := topics.CatalogPrompt(ctx, q)
	extra := 256
	if catalog != "" {
		extra += mlcore.EstimateTokens(catalog) + 64
	}
	if job.RetryInstructions != "" {
		extra += mlcore.EstimateTokens(job.RetryInstructions) + 256
	}
	target := mlcore.CueChunkTarget(mlcore.SystemPrompt(), extra)
	overlap := target / 10
	if overlap > mlcore.DefaultOverlapTokens {
		overlap = mlcore.DefaultOverlapTokens
	}
	chunks := mlcore.ChunkCues(cues, target, overlap)
	slog.Info("context windows starting", "video_id", uuidString(videoID), "job_id", uuidString(job.ID), "chunks", len(chunks), "cues", len(cues), "model", modelName)
	reportContextProgress(ctx, q, job, 0, len(chunks), 0, fmt.Sprintf("Starting %d chunks · %d cues", len(chunks), len(cues)))
	var generated []mlcore.Window
	for i, ch := range chunks {
		var saved []mlcore.Window
		checkpoint, cerr := q.GetContextChunk(ctx, &db.GetContextChunkParams{SetID: set.ID, Ordinal: int32(i)})
		if cerr == nil {
			if err := json.Unmarshal(checkpoint, &saved); err != nil {
				return err
			}
			generated = append(generated, saved...)
			reportContextProgress(ctx, q, job, i+1, len(chunks), len(generated), fmt.Sprintf("Resumed · %d/%d chunks already saved", i+1, len(chunks)))
			continue
		}
		if !errors.Is(cerr, pgx.ErrNoRows) {
			return cerr
		}
		msg := fmt.Sprintf("Generating chunk %d/%d (cues c%04d–c%04d)", i+1, len(chunks), ch.StartID, ch.EndID)
		slog.Info("context windows chunk", "job_id", uuidString(job.ID), "chunk", i+1, "chunks", len(chunks), "cue_start", ch.StartID, "cue_end", ch.EndID, "windows", len(generated))
		reportContextProgress(ctx, q, job, i+1, len(chunks), len(generated), msg)
		user := fmt.Sprintf("Chunk %d/%d (cues c%04d–c%04d):\n%s", i+1, len(chunks), ch.StartID, ch.EndID, ch.Text)
		if catalog != "" {
			user = catalog + "\n\n" + user
		}
		if job.RetryInstructions != "" {
			user = "Review guidance from the user (use it to focus the summary, never invent speech or override the output schema):\n" + job.RetryInstructions + "\n\nTranscript evidence:\n" + user
		}
		models, gerr := ollama.GenerateWindows(ctx, modelName, user)
		if gerr != nil {
			_ = q.UpdateContextWindowSet(ctx, &db.UpdateContextWindowSetParams{
				ID: set.ID, Status: statusForErr(gerr), Metrics: mustJSON(map[string]any{"error": gerr.Error()}),
			})
			return gerr
		}
		saved = mlcore.WindowsFromModel(models, ch.Cues)
		if len(saved) == 0 {
			slog.Error("model windows did not map onto cues", "model_windows", len(models), "cues", len(ch.Cues), "chunk_start", ch.StartID, "chunk_end", ch.EndID)
			_ = q.UpdateContextWindowSet(ctx, &db.UpdateContextWindowSetParams{
				ID: set.ID, Status: "failed", Metrics: mustJSON(map[string]any{"error": "model returned no valid cue windows", "model_windows": len(models)}),
			})
			return fmt.Errorf("model returned no valid cue windows")
		}
		if err := saveFencedContextChunk(ctx, dbc, job, set.ID, int32(i), mustJSON(saved)); err != nil {
			return err
		}
		generated = append(generated, saved...)
		reportContextProgress(ctx, q, job, i+1, len(chunks), len(generated), fmt.Sprintf("Saved chunk %d/%d · %d windows so far", i+1, len(chunks), len(generated)))
		if higher, err := q.HasHigherPriorityMLJob(ctx, job.Priority); err != nil {
			return err
		} else if higher && i+1 < len(chunks) {
			return vision.ErrYield
		}
	}

	// Inference runs outside a transaction. Only publication holds locks; a
	// superseded worker or changed transcript cannot publish stale results.
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = q.LockMLPublication(ctx, &db.LockMLPublicationParams{ID: job.ID, LeaseToken: job.LeaseToken}); err != nil {
		return leaseLost(err)
	}
	if _, err = q.LockVideoForContext(ctx, videoID); err != nil {
		return err
	}
	if err = q.LockTranscriptForContext(ctx, videoID); err != nil {
		return err
	}
	currentHash, err := q.GetTranscriptFingerprint(ctx, videoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("waiting_assets: transcript fingerprint missing")
		}
		return fmt.Errorf("transcript fingerprint: %w", err)
	}
	if hash != currentHash {
		return fmt.Errorf("transcript changed during inference")
	}
	prevRows, err := q.ListContextWindowsByVideo(ctx, videoID)
	if err != nil {
		return err
	}
	prev := windowsFromRows(prevRows)
	sameHash := previousSetSameHash(ctx, q, videoID, set.ID, hash)

	duration := mlcore.DurationFromCues(cues)
	if video != nil && video.DurationSeconds != nil && *video.DurationSeconds > 0 {
		duration = float64(*video.DurationSeconds)
	}
	parents, shorts := mlcore.FlattenGenerated(generated)
	reconciled := mlcore.Reconcile(parents, duration)
	reconciled = mlcore.ApplyOverrides(reconciled, prev, sameHash)
	if sameHash {
		for _, old := range prev {
			if old.Stale || (!old.OverrideBounds && !old.OverrideTitle && !old.OverrideSummary) {
				continue
			}
			preserved := false
			for _, w := range reconciled {
				if (!old.OverrideBounds || (w.Start == old.Start && w.End == old.End)) && (!old.OverrideTitle || w.Title == old.Title) && (!old.OverrideSummary || w.Summary == old.Summary) {
					preserved = true
					break
				}
			}
			if !preserved {
				return fmt.Errorf("manual context edit could not be reconciled; previous generation retained")
			}
		}
	} else if err := q.MarkOverrideWindowsStale(ctx, videoID); err != nil {
		return err
	}
	if err := mlcore.WindowsCovered(reconciled, duration); err != nil {
		return fmt.Errorf("generation incomplete; previous generation retained: %w", err)
	}
	reconciled = mlcore.AttachShorts(reconciled, shorts)

	if err := q.MarkGeneratedWindowsStale(ctx, &db.MarkGeneratedWindowsStaleParams{
		VideoID: videoID, ExceptSetID: pgtype.UUID{},
	}); err != nil {
		return err
	}

	sourceQuery := "ml:" + modelName + ":" + promptVersion
	shortN := 0
	type windowBind struct {
		id               pgtype.UUID
		title            string
		topics, entities []string
	}
	var pendingBinds []windowBind
	for i, w := range reconciled {
		if w.End <= w.Start {
			continue
		}
		parent, ierr := insertGeneratedWindow(ctx, q, videoID, set.ID, sourceQuery, w, "window", pgtype.UUID{}, int32(i+1))
		if ierr != nil {
			return fmt.Errorf("insert window %d: %w", i, ierr)
		}
		pendingBinds = append(pendingBinds, windowBind{id: parent.ID, title: w.Title, topics: w.Topics, entities: w.Entities})
		for j, s := range w.Shorts {
			if s.End <= s.Start {
				continue
			}
			shortN++
			if _, serr := insertGeneratedWindow(ctx, q, videoID, set.ID, sourceQuery, s, "short", parent.ID, 10000+int32(shortN)); serr != nil {
				return fmt.Errorf("insert short %d.%d: %w", i, j, serr)
			}
		}
	}

	metrics := mustJSON(map[string]any{
		"chunks":    len(chunks),
		"windows":   len(reconciled),
		"shorts":    shortN,
		"model":     modelName,
		"digest":    digest,
		"cues":      len(cues),
		"same_hash": sameHash,
	})
	if err := q.UpdateContextWindowSet(ctx, &db.UpdateContextWindowSetParams{
		ID: set.ID, Status: "succeeded", Metrics: metrics,
	}); err != nil {
		return err
	}

	// Enqueue inside the publication transaction. A separate plugin queue
	// transaction here would wait on the video lock held until Commit and
	// deadlock the worker; the durable DB row commits atomically with output.
	if err := q.EnqueueMLJob(ctx, &db.EnqueueMLJobParams{
		VideoID: videoID, Kind: plugin.KindRefine, Priority: 50,
		TranscriptHash: hash, ModelDigest: digest, PromptVersion: promptVersion,
	}); err != nil {
		return fmt.Errorf("enqueue refine_boundaries: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for _, b := range pendingBinds {
		if err := topicStore.BindWindow(ctx, b.id, b.title, b.topics, b.entities); err != nil {
			slog.Warn("bind window topics", "window_id", uuidString(b.id), "error", err)
		}
	}
	if err := topicStore.EnsureStubs(ctx); err != nil {
		slog.Warn("topic stubs", "error", err)
	}
	return nil
}

func reportContextProgress(ctx context.Context, q *db.Queries, job *db.MlJob, chunk, chunks, windows int, message string) {
	slog.Info("context windows progress", "job_id", uuidString(job.ID), "chunk", chunk, "chunks", chunks, "windows", windows, "message", message)
	raw, err := json.Marshal(map[string]any{"phase": "chunk", "chunk": chunk, "chunks": chunks, "windows": windows, "message": message})
	if err != nil {
		return
	}
	if _, err := q.UpdateMLJobProgress(ctx, &db.UpdateMLJobProgressParams{Checkpoint: raw, ID: job.ID, LeaseToken: job.LeaseToken}); err != nil {
		slog.Warn("context progress write failed", "job_id", uuidString(job.ID), "error", err)
	}
}

func saveFencedContextChunk(ctx context.Context, dbc *db.DatabaseConnection, job *db.MlJob, setID pgtype.UUID, ordinal int32, output []byte) error {
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = q.LockMLPublication(ctx, &db.LockMLPublicationParams{ID: job.ID, LeaseToken: job.LeaseToken}); err != nil {
		return leaseLost(err)
	}
	if err = q.SaveContextChunk(ctx, &db.SaveContextChunkParams{SetID: setID, Ordinal: ordinal, Output: output}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func previousSetSameHash(ctx context.Context, q *db.Queries, videoID, currentSet pgtype.UUID, hash string) bool {
	sets, err := q.ListContextWindowSetsForVideo(ctx, videoID)
	if err != nil {
		return true
	}
	for _, s := range sets {
		if s == nil || s.ID == currentSet {
			continue
		}
		return s.TranscriptHash == hash
	}
	return true
}

func insertGeneratedWindow(ctx context.Context, q *db.Queries, videoID, setID pgtype.UUID, sourceQuery string, w mlcore.Window, kind string, parent pgtype.UUID, ordinal int32) (*db.ContextWindow, error) {
	evidence, _ := json.Marshal([]map[string]any{{"cue_start": w.CueStart, "cue_end": w.CueEnd}})
	topics := w.Topics
	if topics == nil {
		topics = []string{}
	}
	entities := w.Entities
	if entities == nil {
		entities = []string{}
	}
	hook := strings.TrimSpace(w.Hook)
	return q.InsertGeneratedContextWindow(ctx, &db.InsertGeneratedContextWindowParams{
		VideoID:               videoID,
		StartTs:               w.Start,
		EndTs:                 w.End,
		Title:                 w.Title,
		Summary:               w.Summary,
		Topics:                topics,
		Entities:              entities,
		SourceQuery:           sourceQuery,
		TranscriptCueEvidence: evidence,
		BoundaryQuality:       "cue",
		SetID:                 setID,
		Ordinal:               ordinal,
		CueStart:              int32(w.CueStart),
		CueEnd:                int32(w.CueEnd),
		GeneratedStartTs:      ptrFloat(w.GeneratedStart),
		GeneratedEndTs:        ptrFloat(w.GeneratedEnd),
		Confidence:            ptrFloat(w.Confidence),
		OverrideTitle:         w.OverrideTitle,
		OverrideSummary:       w.OverrideSummary,
		OverrideBounds:        w.OverrideBounds,
		Kind:                  kind,
		ParentID:              parent,
		Hook:                  hook,
	})
}

func windowsFromRows(rows []*db.ContextWindow) []mlcore.Window {
	out := make([]mlcore.Window, 0, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		if r.Kind == "short" {
			continue
		}
		w := mlcore.Window{
			Start:           r.StartTs,
			End:             r.EndTs,
			Title:           r.Title,
			Summary:         r.Summary,
			Topics:          append([]string(nil), r.Topics...),
			Entities:        append([]string(nil), r.Entities...),
			CueStart:        int(r.CueStart),
			CueEnd:          int(r.CueEnd),
			OverrideTitle:   r.OverrideTitle,
			OverrideSummary: r.OverrideSummary,
			OverrideBounds:  r.OverrideBounds,
			Stale:           r.Stale,
		}
		if r.GeneratedStartTs != nil && r.GeneratedEndTs != nil {
			w.GeneratedStart = *r.GeneratedStartTs
			w.GeneratedEnd = *r.GeneratedEndTs
			w.HasGeneratedSpan = true
		}
		if r.Confidence != nil {
			w.Confidence = *r.Confidence
		}
		out = append(out, w)
	}
	return out
}

func ptrFloat(v float64) *float64 { return &v }

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

func statusForErr(err error) string {
	if mlcore.IsWaiting(err) {
		return "waiting_model"
	}
	return "failed"
}

// acquireRunnableContextModel picks an installed model, skipping any that already
// killed llama-server, then canaries it. On infrastructure failure it tries the
// next smaller installed challenger in the same job.
func acquireRunnableContextModel(ctx context.Context, ollama *mlcore.Ollama, primary string, challengers []string) (modelName, digest string, release func(), err error) {
	skip := map[string]struct{}{}
	for {
		nextPrimary, nextChallengers := remainingContextModels(primary, challengers, skip)
		if nextPrimary == "" && len(nextChallengers) == 0 {
			return "", "", nil, fmt.Errorf("%w: no installed context model survived canary", mlcore.ErrWaitingModel)
		}
		modelName, digest, err = mlcore.PickInstalledModel(ctx, ollama, nextPrimary, nextChallengers)
		if err != nil {
			return "", "", nil, err
		}
		key := strings.ToLower(strings.TrimSpace(modelName))
		if _, dup := skip[key]; dup {
			return "", "", nil, fmt.Errorf("%w: %s still selected after infrastructure failure", mlcore.ErrWaitingModel, modelName)
		}
		skip[key] = struct{}{}
		release, err = ollama.ShareLoaded(ctx, modelName, digest, func() (func(), error) {
			rel, aerr := modelruntime.AcquireOllama(ctx, modelName)
			if aerr != nil {
				return nil, fmt.Errorf("waiting_model: %w", aerr)
			}
			return rel, nil
		})
		if err != nil {
			if errors.Is(err, mlcore.ErrInfrastructure) && ollama.Resident() {
				slog.Warn("context model unusable; trying smaller installed challenger", "model", modelName, "error", err)
				continue
			}
			return "", "", nil, err
		}
		return modelName, digest, release, nil
	}
}

func remainingContextModels(primary string, challengers []string, skip map[string]struct{}) (string, []string) {
	keep := func(name string) bool {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			return false
		}
		_, skipped := skip[name]
		return !skipped
	}
	if !keep(primary) {
		primary = ""
	}
	out := make([]string, 0, len(challengers))
	for _, c := range challengers {
		if keep(c) {
			out = append(out, c)
		}
	}
	return primary, out
}
