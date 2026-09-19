package ingest

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"os"
	"thirdcoast.systems/rewind/internal/db"
)

type mediaPublisher interface {
	PublishVideoMedia(context.Context, *db.PublishVideoMediaParams) (int64, error)
}

// publishIngestMedia is deliberately independent of thumbnail, waveform and ML work.
func publishIngestMedia(ctx context.Context, q mediaPublisher, id pgtype.UUID, path string, thumbnail, hash *string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("source media is empty or not a regular file: %s", path)
	}
	size := info.Size()
	n, err := q.PublishVideoMedia(ctx, &db.PublishVideoMediaParams{ID: id, VideoPath: &path, ThumbnailPath: thumbnail, FileHash: hash, FileSize: &size})
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("media publication updated %d videos", n)
	}
	return nil
}
