package ingest

import (
	"context"

	"thirdcoast.systems/rewind/internal/runtimecfg"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

func withSharedFFmpeg(ctx context.Context) context.Context {
	return ffmpeg.WithHostShare(ctx, runtimecfg.Int(ctx, "processing.ffmpeg_threads"))
}
