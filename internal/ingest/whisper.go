package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/transcription"
	"thirdcoast.systems/rewind/pkg/captions"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// enqueueTranscribeJob queues background ASR on rewind-ml. These jobs run on CPU
// so ingest cannot pin the host GPU. Interactive caption regen uses enqueueInteractiveTranscribeJob.
func enqueueTranscribeJob(ctx context.Context, q *db.Queries, videoID pgtype.UUID) error {
	return enqueueTranscribe(ctx, q, videoID, 160, transcription.CPUPromptVersion)
}

func enqueueInteractiveTranscribeJob(ctx context.Context, q *db.Queries, videoID pgtype.UUID) error {
	return enqueueTranscribe(ctx, q, videoID, 10, "")
}

func enqueueTranscribe(ctx context.Context, q *db.Queries, videoID pgtype.UUID, priority int32, promptVersion string) error {
	_ = q
	return plugin.Enqueue(ctx, plugin.Job{
		VideoID:       videoID.String(),
		Kind:          plugin.KindTranscribe,
		Priority:      priority,
		PromptVersion: promptVersion,
	})
}

func findCanonicalCaptionFilePath(dir string, videoID string) (string, string, bool) {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(videoID) == "" {
		return "", "", false
	}
	candidates := []struct {
		path string
		lang string
	}{
		{path: filepath.Join(dir, videoID+".captions.en.vtt"), lang: "en"},
		{path: filepath.Join(dir, videoID+".captions.und.vtt"), lang: "und"},
	}
	for _, c := range candidates {
		if _, err := os.Stat(c.path); err == nil {
			return c.path, c.lang, true
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, videoID+".captions.*.vtt"))
	for _, p := range matches {
		base := strings.ToLower(filepath.Base(p))
		if strings.HasSuffix(base, ".src.vtt") {
			continue
		}
		return p, captions.LangFromFilename(p), true
	}
	return "", "", false
}
