package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/comments"
	"thirdcoast.systems/rewind/internal/db"
)

// ingestCommentsFromInfoJSON delegates to the shared comments ingester (used by
// both the initial ingest and the downloader's comment catch-up loop).
func ingestCommentsFromInfoJSON(ctx context.Context, q *db.Queries, videoID pgtype.UUID, source string, rawInfoJSON []byte) error {
	return comments.IngestFromInfoJSON(ctx, q, videoID, source, rawInfoJSON)
}

func findLiveChatFile(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.live_chat.json"))
	for _, p := range matches {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			return p
		}
	}
	return ""
}
