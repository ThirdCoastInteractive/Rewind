// Package archival turns a user-submitted URL into the right kind of download
// job: a single-video job, or a playlist/channel job that the downloader
// expands into one child job per contained video.
package archival

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
)

// EnqueueResult reports what EnqueueURL created.
type EnqueueResult struct {
	Job        *db.DownloadJob
	IsPlaylist bool // a playlist/channel job (expanded into children by the downloader)
	Refresh    bool // single-video job for an already-archived source (metadata refresh)
}

// EnqueueURL enqueues a user-submitted URL for archival. Playlist/channel URLs
// become a "playlist" job; any other URL becomes a single-video job, with
// refresh=true when that exact source URL is already archived.
func EnqueueURL(ctx context.Context, q *db.Queries, rawURL string, archivedBy pgtype.UUID) (*EnqueueResult, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("url is required")
	}

	if videoid.IsPlaylistOrChannelURL(rawURL) {
		job, err := q.EnqueuePlaylistJob(ctx, &db.EnqueuePlaylistJobParams{
			URL:        rawURL,
			ArchivedBy: archivedBy,
		})
		if err != nil {
			return nil, err
		}
		return &EnqueueResult{Job: job, IsPlaylist: true}, nil
	}

	refresh := false
	if existing, err := q.SelectVideoBySrc(ctx, rawURL); err == nil && existing != nil {
		refresh = true
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	job, err := q.EnqueueDownloadJob(ctx, &db.EnqueueDownloadJobParams{
		URL:        rawURL,
		ArchivedBy: archivedBy,
		Refresh:    refresh,
		ExtraArgs:  []string{},
	})
	if err != nil {
		return nil, err
	}
	return &EnqueueResult{Job: job, Refresh: refresh}, nil
}
