package archive

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// VideoBlobKey is videos.video_path for videoID.
func VideoBlobKey(ctx context.Context, videoID string) (string, error) {
	dbc, err := boundDB()
	if err != nil {
		return "", err
	}
	id, err := parsePGUUID(videoID)
	if err != nil {
		return "", err
	}
	var path *string
	err = dbc.QueryRow(ctx, `SELECT video_path FROM videos WHERE id = $1`, id).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("archive: video not found")
	}
	if err != nil {
		return "", err
	}
	if path == nil || strings.TrimSpace(*path) == "" {
		return "", fmt.Errorf("archive: video has no blob")
	}
	return strings.TrimSpace(*path), nil
}
