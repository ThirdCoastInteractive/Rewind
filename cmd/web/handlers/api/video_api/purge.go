package video_api

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func purgeGeneratedMedia(ctx context.Context, dbc *db.DatabaseConnection, video *db.Video) {
	if video == nil || dbc == nil {
		return
	}
	for _, path := range clipExportPathsForVideo(ctx, dbc, video.ID) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("remove clip export", "path", path, "error", err)
		}
	}
	videoPath := ""
	if video.VideoPath != nil {
		videoPath = *video.VideoPath
	}
	plugin.RemoveVideoBlobs(ctx, video.ID.String(), videoPath)
	if dir, ok := safeVideoDirForDeletion(video.ID); ok {
		if err := os.RemoveAll(dir); err != nil {
			slog.Warn("remove video dir", "dir", dir, "error", err)
		}
	}
}

func clipExportPathsForVideo(ctx context.Context, dbc *db.DatabaseConnection, videoID pgtype.UUID) []string {
	rows, err := dbc.Query(ctx, `SELECT ce.file_path FROM clip_exports ce JOIN clips c ON c.id = ce.clip_id WHERE c.video_id = $1 AND ce.file_path <> ''`, videoID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			continue
		}
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
