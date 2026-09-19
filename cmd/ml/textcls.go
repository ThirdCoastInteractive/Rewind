package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/cmd/ml/internal/mlcore"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/runtimecfg"
	"thirdcoast.systems/rewind/internal/textcls"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func textclsDigest(ctx context.Context, c *textcls.Client) (string, error) {
	sentiment := runtimecfg.String(ctx, "textcls.sentiment_model")
	toxicity := runtimecfg.String(ctx, "textcls.toxicity_model")
	var models []textcls.Model
	if c != nil && c.URL != "" {
		resp, err := c.Models(ctx)
		if err == nil {
			models = resp.Models
		}
	}
	return textcls.DigestForSettings(sentiment, toxicity, models), nil
}

func enqueueTextcls(ctx context.Context, dbc *db.DatabaseConnection) error {
	c := textcls.FromEnv()
	if c.URL == "" {
		return nil
	}
	digest, err := textclsDigest(ctx, c)
	if err != nil {
		return err
	}
	q := dbc.Queries(ctx)
	commentVideos, err := textcls.ListVideosNeedingCommentClassify(ctx, dbc.Pool, digest, 200)
	if err != nil {
		// Tables from 00087 may not exist yet; skip quietly.
		if strings.Contains(err.Error(), "comment_scores") || strings.Contains(err.Error(), "does not exist") {
			slog.Debug("textcls comment enqueue skipped", "error", err)
		} else {
			return err
		}
	} else {
		for _, videoID := range commentVideos {
			hash, herr := textcls.CommentBatchHashForVideo(ctx, dbc.Pool, videoID)
			if herr != nil {
				continue
			}
			if e := plugin.Enqueue(ctx, plugin.Job{
				VideoID: uuid.UUID(videoID.Bytes).String(), Kind: plugin.KindClassify, Priority: textcls.Priority,
				TranscriptHash: hash, ModelDigest: digest, PromptVersion: textcls.PromptVersion,
			}); e != nil {
				return e
			}
		}
	}

	speechVideos, err := textcls.ListVideosNeedingSpeechTone(ctx, dbc.Pool, digest, 200)
	if err != nil {
		if strings.Contains(err.Error(), "speech_scores") || strings.Contains(err.Error(), "does not exist") {
			slog.Debug("textcls speech enqueue skipped", "error", err)
			return nil
		}
		return err
	}
	for _, videoID := range speechVideos {
		hash, herr := q.GetTranscriptFingerprint(ctx, videoID)
		if herr != nil {
			continue
		}
		if e := plugin.Enqueue(ctx, plugin.Job{
			VideoID: uuid.UUID(videoID.Bytes).String(), Kind: plugin.KindSpeechTone, Priority: textcls.Priority,
			TranscriptHash: hash, ModelDigest: digest, PromptVersion: textcls.PromptVersion,
		}); e != nil {
			return e
		}
	}
	return nil
}

func handleCommentClassify(ctx context.Context, dbc *db.DatabaseConnection, job *db.MlJob) error {
	c := textcls.FromEnv()
	if c.URL == "" {
		return fmt.Errorf("waiting_model: TEXTCLS_URL is not configured")
	}
	sentiment := runtimecfg.String(ctx, "textcls.sentiment_model")
	toxicity := runtimecfg.String(ctx, "textcls.toxicity_model")
	models, err := c.Models(ctx)
	if err != nil {
		return fmt.Errorf("waiting_model: %w", err)
	}
	if !textcls.ModelsReady(models.Models, sentiment, toxicity) {
		return fmt.Errorf("waiting_model: textcls models not installed")
	}
	digest := textcls.DigestForSettings(sentiment, toxicity, models.Models)

	comments, err := textcls.ListUnscoredCommentsForVideo(ctx, dbc.Pool, job.VideoID, digest)
	if err != nil {
		return err
	}
	if len(comments) == 0 {
		return nil
	}
	for i := 0; i < len(comments); i += textcls.MaxBatch {
		end := i + textcls.MaxBatch
		if end > len(comments) {
			end = len(comments)
		}
		batch := comments[i:end]
		items := make([]textcls.Item, 0, len(batch))
		for _, row := range batch {
			items = append(items, textcls.Item{ID: uuid.UUID(row.ID.Bytes).String(), Text: row.Text})
		}
		scored, err := c.Classify(ctx, items, sentiment, toxicity)
		if err != nil {
			return err
		}
		for _, item := range scored {
			var id pgtype.UUID
			if err := id.Scan(item.ID); err != nil {
				return err
			}
			if err := textcls.UpsertCommentScore(ctx, dbc.Pool, id, digest, item.Sentiment, item.Toxicity, textcls.LabelsJSON(item.Labels)); err != nil {
				return err
			}
		}
	}
	return nil
}

func handleSpeechTone(ctx context.Context, dbc *db.DatabaseConnection, job *db.MlJob) error {
	c := textcls.FromEnv()
	if c.URL == "" {
		return fmt.Errorf("waiting_model: TEXTCLS_URL is not configured")
	}
	q := dbc.Queries(ctx)
	hash, err := q.GetTranscriptFingerprint(ctx, job.VideoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("waiting_assets: transcript fingerprint missing")
		}
		return err
	}
	if job.TranscriptHash != "" && job.TranscriptHash != hash {
		return mlcore.ErrSuperseded
	}
	sentiment := runtimecfg.String(ctx, "textcls.sentiment_model")
	toxicity := runtimecfg.String(ctx, "textcls.toxicity_model")
	models, err := c.Models(ctx)
	if err != nil {
		return fmt.Errorf("waiting_model: %w", err)
	}
	if !textcls.ModelsReady(models.Models, sentiment, toxicity) {
		return fmt.Errorf("waiting_model: textcls models not installed")
	}
	digest := textcls.DigestForSettings(sentiment, toxicity, models.Models)

	windows, err := textcls.ListContextWindowsForSpeechTone(ctx, dbc.Pool, job.VideoID)
	if err != nil && !strings.Contains(err.Error(), "does not exist") {
		return err
	}
	if len(windows) == 0 {
		windows, err = speechWindowsFromCues(ctx, q, job.VideoID)
		if err != nil {
			return err
		}
	}
	if len(windows) == 0 {
		return nil
	}
	for i := 0; i < len(windows); i += textcls.MaxBatch {
		end := i + textcls.MaxBatch
		if end > len(windows) {
			end = len(windows)
		}
		batch := windows[i:end]
		items := make([]textcls.Item, 0, len(batch))
		for j, w := range batch {
			items = append(items, textcls.Item{ID: fmt.Sprintf("%d", i+j), Text: w.Text})
		}
		scored, err := c.Classify(ctx, items, sentiment, toxicity)
		if err != nil {
			return err
		}
		for j, item := range scored {
			w := batch[j]
			if err := textcls.UpsertSpeechScore(ctx, dbc.Pool, job.VideoID, w.SetID, w.StartTS, w.EndTS, digest, item.Sentiment, item.Toxicity, textcls.LabelsJSON(item.Labels)); err != nil {
				return err
			}
		}
	}
	return nil
}

func speechWindowsFromCues(ctx context.Context, q *db.Queries, videoID pgtype.UUID) ([]textcls.SpeechWindow, error) {
	cues, _, err := loadTranscriptCues(ctx, q, videoID)
	if err != nil {
		return nil, fmt.Errorf("waiting_assets: %w", err)
	}
	if len(cues) == 0 {
		return nil, nil
	}
	const span = 60.0
	var out []textcls.SpeechWindow
	var buf []string
	start := cues[0].Start
	end := cues[0].End
	flush := func() {
		if len(buf) == 0 {
			return
		}
		out = append(out, textcls.SpeechWindow{
			StartTS: start,
			EndTS:   end,
			Text:    strings.Join(buf, " "),
		})
		buf = nil
	}
	for _, cue := range cues {
		if len(buf) > 0 && cue.Start-start >= span {
			flush()
		}
		if len(buf) == 0 {
			start = cue.Start
			end = cue.End
		}
		buf = append(buf, strings.TrimSpace(cue.Text))
		if cue.End > end {
			end = cue.End
		}
	}
	flush()
	return out, nil
}
