package vision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/frames"
)

// ErrYield means a checkpoint was stored and the worker should claim again.
var ErrYield = errors.New("checkpoint saved; yielding GPU")

// IndexJob embeds one bounded batch of frames for a visual_index ML job.
func IndexJob(ctx context.Context, dbc *db.DatabaseConnection, job *db.MlJob) error {
	q := dbc.Queries(ctx)
	c := FromEnv()
	model, err := ModelReady(ctx, q, c, CLIPModel)
	if err != nil {
		return err
	}
	if model.Fingerprint != job.ModelDigest {
		return fmt.Errorf("model fingerprint changed; enqueue a new index")
	}
	asset, err := Asset(ctx, q, job.VideoID)
	if err != nil {
		return err
	}
	if asset.Duration <= 0 {
		return fmt.Errorf("video duration unavailable")
	}
	fingerprint := frames.Fingerprint(asset)
	start, end, interval := 0.0, asset.Duration, 5.0
	var config struct{ Start, End, Interval float64 }
	if len(job.Checkpoint) > 0 {
		_ = json.Unmarshal(job.Checkpoint, &config)
		if config.Interval > 0 {
			start, end, interval = config.Start, config.End, config.Interval
		}
	}
	if start < 0 || interval < 1 {
		return fmt.Errorf("invalid indexing range")
	}
	if end > asset.Duration {
		end = asset.Duration
	}
	if end <= start {
		return fmt.Errorf("invalid indexing range")
	}
	set, err := q.EnsureVisualIndexSet(ctx, &db.EnsureVisualIndexSetParams{
		VideoID: job.VideoID, ModelID: model.Fingerprint, AssetFingerprint: fingerprint,
		IntervalSeconds: interval, StartTs: start, EndTs: end,
	})
	if err != nil {
		return err
	}
	setID, next := set.ID, set.NextSample
	total := int(math.Ceil((end - start) / interval))
	times := sampleTimes(start, end, interval, int(next), 32)
	if len(times) == 0 {
		if set.Status == "succeeded" {
			return nil
		}
		return fmt.Errorf("waiting_assets: indexing range produced no samples")
	}
	fs, err := extractIndexFrames(ctx, asset, times, interval)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(fs))
	images := make([][]byte, 0, len(fs))
	kept := make([]int, 0, len(fs))
	assetError := ""
	for i, f := range fs {
		if f.Error != "" {
			assetError = f.Error
			continue
		}
		ids = append(ids, strconv.Itoa(int(next)+i))
		images = append(images, f.JPEG)
		kept = append(kept, i)
	}
	if len(images) == 0 {
		if assetError == "" {
			assetError = "no usable frames in this batch"
		}
		return fmt.Errorf("waiting_assets: %s", assetError)
	}
	results, err := c.Batch(ctx, ids, images)
	if err != nil {
		return err
	}
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = q.LockMLPublication(ctx, &db.LockMLPublicationParams{ID: job.ID, LeaseToken: job.LeaseToken}); err != nil {
		return err
	}
	if frames.Fingerprint(asset) != fingerprint {
		return fmt.Errorf("assets changed during indexing; retry against the new assets")
	}
	for i, result := range results {
		if len(result.Error) > 0 && string(result.Error) != "null" {
			return fmt.Errorf("vision sample %s: %s", result.ID, result.Error)
		}
		f := fs[kept[i]]
		vector, err := Normalize(result.Result.CLIP)
		if err != nil {
			return err
		}
		if err = q.SaveVisualEmbedding(ctx, &db.SaveVisualEmbeddingParams{
			SetID: setID, SampleIndex: next + int32(kept[i]), SampleTs: f.Sample, FrameRef: f.Reference, Embedding: vector,
		}); err != nil {
			return err
		}
	}
	next += int32(len(fs))
	status := "processing"
	if int(next) >= total || len(sampleTimes(start, end, interval, int(next), 1)) == 0 {
		status = "succeeded"
		assetError = ""
	}
	if status == "succeeded" {
		if err = q.RetireVisualIndexes(ctx, setID); err != nil {
			return err
		}
	}
	if err = q.AdvanceVisualIndex(ctx, &db.AdvanceVisualIndexParams{ID: setID, NextSample: next, Status: status, Error: assetError}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if status != "succeeded" {
		return ErrYield
	}
	return nil
}

func sampleTimes(start, end, interval float64, next, max int) []float64 {
	if interval < 1 || max < 1 || next < 0 || end <= start {
		return nil
	}
	total := int(math.Ceil((end - start) / interval))
	n := min(max, total-next)
	if n <= 0 {
		return nil
	}
	out := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		t := start + float64(next+i)*interval
		if t >= end {
			break
		}
		out = append(out, t)
	}
	return out
}

func extractIndexFrames(ctx context.Context, asset frames.Asset, times []float64, interval float64) ([]frames.Frame, error) {
	if asset.File != "" {
		fs, err := frames.Default.Sample(ctx, asset, times[0], interval, len(times))
		if err == nil {
			return fs, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
	}
	return frames.Default.Get(ctx, asset, times, "preview", 960)
}
