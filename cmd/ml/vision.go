package main

import (
	"context"
	"thirdcoast.systems/rewind/internal/runtimecfg"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/frames"
	"thirdcoast.systems/rewind/internal/vision"
	"thirdcoast.systems/rewind/pkg/plugin"

	"github.com/google/uuid"
)

func enqueueVision(ctx context.Context, dbc *db.DatabaseConnection) error {
	c := vision.FromEnv()
	if c.URL == "" {
		return nil
	}
	q := dbc.Queries(ctx)
	if runtimecfg.Env(ctx, "VISUAL_BACKFILL_ENABLED") != "true" {
		return nil
	}
	m, e := vision.ModelReady(ctx, q, c, vision.CLIPModel)
	if e != nil {
		return nil
	}
	videos, e := q.ListVisualCandidates(ctx)
	if e != nil {
		return e
	}
	for _, v := range videos {
		if e = q.RecordVisionAssetCheck(ctx, &db.RecordVisionAssetCheckParams{VideoID: v.ID, Kind: "visual_index"}); e != nil {
			return e
		}
		a, e := vision.Asset(ctx, q, v.ID)
		if e != nil {
			continue
		}
		if e = plugin.Enqueue(ctx, plugin.Job{VideoID: uuid.UUID(v.ID.Bytes).String(), Kind: plugin.KindEmbed, Priority: 500, TranscriptHash: frames.Fingerprint(a), ModelDigest: m.Fingerprint, PromptVersion: "samples-5s-v1"}); e != nil {
			return e
		}
	}
	return nil
}

func handleVision(ctx context.Context, dbc *db.DatabaseConnection, job *db.MlJob) error {
	return vision.IndexJob(ctx, dbc, job)
}
