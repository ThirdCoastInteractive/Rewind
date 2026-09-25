package plugin

import (
	"context"
	"errors"
	"log/slog"
	"strings"
)

// RemoveVideoBlobs deletes blob objects for a library row: the stored
// video_path, every key under videoID/, and the parent prefix of video_path
// when that is a Stream-UID master (live archive).
func RemoveVideoBlobs(ctx context.Context, videoID, videoPath string) {
	b := Blobs()
	if b == nil {
		return
	}
	seen := map[string]struct{}{}
	remove := func(key string) {
		key = strings.TrimPrefix(strings.TrimSpace(key), "/")
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		if err := b.Remove(ctx, key); err != nil && !errors.Is(err, ErrNotFound) {
			slog.Warn("blob remove", "key", key, "error", err)
		}
	}
	prefixes := []string{strings.Trim(videoID, "/") + "/"}
	if videoPath != "" {
		remove(videoPath)
		if i := strings.LastIndex(videoPath, "/"); i > 0 {
			p := videoPath[:i+1]
			if p != prefixes[0] {
				prefixes = append(prefixes, p)
			}
		}
	}
	for _, prefix := range prefixes {
		keys, err := b.List(ctx, prefix)
		if err != nil {
			slog.Warn("blob list", "prefix", prefix, "error", err)
			continue
		}
		for _, k := range keys {
			remove(k)
		}
	}
}
