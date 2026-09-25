package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"golang.org/x/text/language"
	"os"

	"thirdcoast.systems/rewind/internal/contextwindow"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/transcription"
	"thirdcoast.systems/rewind/pkg/captions"
)

func handleTranscriptRepair(ctx context.Context, dbc *db.DatabaseConnection, job *db.MlJob, videoPath string) error {
	if job.RangeStart == nil || job.RangeEnd == nil {
		return fmt.Errorf("repair needs an audio range")
	}
	q := dbc.Queries(ctx)
	// Backup, replacement and context enqueue commit together. A worker replay
	// after that commit must not re-transcribe or enqueue another generation.
	published, err := q.TranscriptRepairPublished(ctx, job.ID)
	if err != nil {
		return err
	}
	if published {
		return nil
	}
	old, err := q.GetVideoTranscript(ctx, job.VideoID)
	if err != nil {
		return err
	}
	if len(old.Coverage) > 0 {
		return fmt.Errorf("range repair requires an existing complete transcript")
	}
	scratch, err := os.MkdirTemp("", "rewind-repair-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch) // Only newly created temporary ASR output.
	cfg := loadWhisperConfig(ctx)
	base, _ := language.Tag(old.Lang).Base()
	cfg.Language = base.String()
	if cfg.Language == "und" {
		cfg.Language = "auto"
	}
	cfg.Translate = false
	cfg.Extra = append(cfg.Extra, "-mc", "0") // Reset decoder text context; never feed broken lyrics back in.
	var replacement []captions.Cue
	for start := *job.RangeStart; start < *job.RangeEnd; start += 300 {
		end := min(start+300, *job.RangeEnd)
		path, _, err := generateCaptionsWithWhisperConfig(ctx, videoPath, job.VideoID.String(), scratch, cfg, start, end)
		if err != nil {
			return err
		}
		doc, err := captions.CleanFile(path)
		if err != nil {
			return err
		}
		for _, cue := range doc.Cues {
			cue.Start = max(start, start+cue.Start)
			cue.End = min(end, start+cue.End)
			if cue.End > cue.Start {
				replacement = append(replacement, cue)
			}
		}
	}
	if len(replacement) == 0 {
		return fmt.Errorf("repair recognized no speech; original transcript retained")
	}
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = q.LockMLPublication(ctx, &db.LockMLPublicationParams{ID: job.ID, LeaseToken: job.LeaseToken}); err != nil {
		return leaseLost(err)
	}
	if _, err = q.LockVideoForContext(ctx, job.VideoID); err != nil {
		return err
	}
	if err = q.LockTranscriptRepair(ctx, job.VideoID); err != nil {
		return err
	}
	hash, err := q.GetTranscriptFingerprint(ctx, job.VideoID)
	if err != nil {
		return err
	}
	if hash != job.TranscriptHash {
		return fmt.Errorf("transcript changed during repair; original retained, request repair again")
	}
	var previous []captions.Cue
	if err := json.Unmarshal(old.Cues, &previous); err != nil {
		return err
	}
	merged := transcription.ReplaceRange(previous, replacement, *job.RangeStart, *job.RangeEnd)
	raw, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	if err := q.BackupTranscriptRepair(ctx, &db.BackupTranscriptRepairParams{JobID: job.ID, VideoID: job.VideoID, Lang: old.Lang}); err != nil {
		return err
	}
	n, err := q.ReplaceTranscriptCues(ctx, &db.ReplaceTranscriptCuesParams{VideoID: job.VideoID, Lang: old.Lang, Text: captions.PlainText(merged), Cues: raw})
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("transcript missing during repair")
	}
	if _, err := contextwindow.EnqueueRetry(ctx, q, job.VideoID, job.RetryInstructions); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if e := maybeEnqueueDiarize(ctx, dbc, job.VideoID); e != nil {
		slog.Warn("enqueue diarize", "video_id", uuidString(job.VideoID), "error", e)
	}
	return nil
}
