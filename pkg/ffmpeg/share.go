package ffmpeg

import (
	"context"
	"strconv"
)

type hostShareKey struct{}

type hostShare struct {
	Threads int
}

// WithHostShare marks ffmpeg work as background ingest/asset generation.
// Those runs never take NVDEC/NVENC, cap thread count, and share a single
// process-wide ffmpeg slot so they cannot starve the host video decoder.
func WithHostShare(ctx context.Context, threads int) context.Context {
	if threads < 1 {
		threads = 2
	}
	if threads > 8 {
		threads = 8
	}
	return context.WithValue(ctx, hostShareKey{}, hostShare{Threads: threads})
}

func hostShareFrom(ctx context.Context) (hostShare, bool) {
	if ctx == nil {
		return hostShare{}, false
	}
	cfg, ok := ctx.Value(hostShareKey{}).(hostShare)
	return cfg, ok
}

var hostShareGate = make(chan struct{}, 1)

func acquireHostShare(ctx context.Context) (func(), error) {
	if _, ok := hostShareFrom(ctx); !ok {
		return func() {}, nil
	}
	select {
	case hostShareGate <- struct{}{}:
		return func() { <-hostShareGate }, nil
	case <-ctx.Done():
		return func() {}, ctx.Err()
	}
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func insertBeforeInput(args, insert []string) []string {
	if len(insert) == 0 {
		return args
	}
	for i := 0; i < len(args); i++ {
		if args[i] == "-i" {
			out := make([]string, 0, len(args)+len(insert))
			out = append(out, args[:i]...)
			out = append(out, insert...)
			out = append(out, args[i:]...)
			return out
		}
	}
	out := make([]string, 0, len(args)+len(insert))
	out = append(out, insert...)
	out = append(out, args...)
	return out
}

// prepareArgs applies host-share limits only for background ingest/asset work.
func prepareArgs(ctx context.Context, args []string) []string {
	cfg, share := hostShareFrom(ctx)
	if !share {
		return args
	}
	insert := make([]string, 0, 6)
	if !hasFlag(args, "-hwaccel") {
		insert = append(insert, "-hwaccel", "none")
	}
	if cfg.Threads > 0 {
		n := strconv.Itoa(cfg.Threads)
		if !hasFlag(args, "-threads") {
			insert = append(insert, "-threads", n)
		}
		if !hasFlag(args, "-filter_threads") {
			insert = append(insert, "-filter_threads", n)
		}
	}
	return insertBeforeInput(args, insert)
}
