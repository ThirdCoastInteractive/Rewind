// Package archival turns a user-submitted URL into the right kind of download
// job: a single-video job, or a playlist/channel job that the downloader
// expands into one child job per contained video.
package archival

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
)

// MaxWaitForVideoSeconds caps --wait-for-video from API clients.
const MaxWaitForVideoSeconds = 86400

// LiveExtraArgs builds download-job extra_args for live archive intent.
// Does not add --no-live-from-start, --downloader, or -f; the worker adds
// those after probing.
func LiveExtraArgs(fromStart bool, waitSeconds int, mediaURL string) []string {
	args := make([]string, 0, 4)
	if fromStart {
		args = append(args, "--live-from-start")
	}
	if waitSeconds > 0 {
		if waitSeconds > MaxWaitForVideoSeconds {
			waitSeconds = MaxWaitForVideoSeconds
		}
		args = append(args, "--wait-for-video", strconv.Itoa(waitSeconds))
	}
	if media := strings.TrimSpace(mediaURL); media != "" {
		if u, err := url.Parse(media); err == nil &&
			(u.Scheme == "http" || u.Scheme == "https") &&
			u.Host != "" {
			args = append(args, "--rewind-media-url", media)
		}
	}
	return args
}

// EnqueueResult reports what EnqueueURL created.
type EnqueueResult struct {
	Job        *db.DownloadJob
	IsPlaylist bool // a playlist/channel job (expanded into children by the downloader)
	Refresh    bool // single-video job for an already-archived source (metadata refresh)
	Reused     bool // an existing useful job was returned instead of creating a duplicate
}

// EnqueueOptions controls optional EnqueueURLOpts behavior.
type EnqueueOptions struct {
	ExtraArgs []string
}

// EnqueueURL enqueues a user-submitted URL for archival. Playlist/channel URLs
// become a "playlist" job; any other URL becomes a canonicalized single-video
// job. Repeated submissions reuse an active or succeeded job. Explicit retry
// and redownload actions bypass this helper.
func EnqueueURL(ctx context.Context, q *db.Queries, rawURL string, archivedBy pgtype.UUID) (*EnqueueResult, error) {
	return EnqueueURLOpts(ctx, q, rawURL, archivedBy, EnqueueOptions{})
}

// EnqueueURLOpts is EnqueueURL with options. ExtraArgs are ignored for
// playlist/channel collection jobs.
func EnqueueURLOpts(ctx context.Context, q *db.Queries, rawURL string, archivedBy pgtype.UUID, opts EnqueueOptions) (*EnqueueResult, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("url is required")
	}

	// Live channel pages before playlist classification so YouTube /…/live is
	// not treated as a collection expansion job.
	if videoid.IsLiveChannelURL(rawURL) {
		if normalized, _, err := videoid.NormalizeSourceURL(rawURL); err == nil {
			rawURL = normalized
		}
		return enqueueLiveChannelURL(ctx, q, rawURL, archivedBy, opts.ExtraArgs)
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

	if normalized, _, err := videoid.NormalizeSourceURL(rawURL); err == nil {
		rawURL = normalized
	}

	extraArgs := opts.ExtraArgs
	if extraArgs == nil {
		extraArgs = []string{}
	}

	refresh := false
	if existing, err := q.SelectVideoBySrc(ctx, rawURL); err == nil && existing != nil {
		// A metadata catalog row is only a placeholder. Archiving it must run a
		// full media download so ingest upgrades the same deterministic record.
		refresh = existing.Media != "metadata"
		if refresh {
			job, jobErr := q.GetReusableDownloadJobForVideo(ctx, &db.GetReusableDownloadJobForVideoParams{
				VideoID:  existing.ID,
				VideoSrc: existing.Src,
			})
			if jobErr == nil {
				return &EnqueueResult{Job: job, Reused: true}, nil
			}
			if !errors.Is(jobErr, pgx.ErrNoRows) {
				return nil, jobErr
			}
		}
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	enqueued, err := q.EnqueueDownloadJobOnce(ctx, &db.EnqueueDownloadJobOnceParams{
		URL:        rawURL,
		ArchivedBy: archivedBy,
		Refresh:    refresh,
		ExtraArgs:  extraArgs,
	})
	if err != nil {
		return nil, err
	}
	job, err := q.GetDownloadJobByID(ctx, enqueued.ID)
	if err != nil {
		return nil, err
	}
	return &EnqueueResult{Job: job, Refresh: refresh && !enqueued.Reused, Reused: enqueued.Reused}, nil
}

// enqueueLiveChannelURL reuses only an active (queued/processing) job for the
// channel URL. Succeeded jobs are ignored so a later live session can record.
func enqueueLiveChannelURL(ctx context.Context, q *db.Queries, rawURL string, archivedBy pgtype.UUID, extraArgs []string) (*EnqueueResult, error) {
	job, err := q.GetReusableDownloadJobForVideo(ctx, &db.GetReusableDownloadJobForVideoParams{
		VideoID:  pgtype.UUID{}, // NULL — match by canonical channel URL only
		VideoSrc: rawURL,
	})
	if err == nil {
		if liveChannelJobReusable(job) {
			return &EnqueueResult{Job: job, Reused: true}, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	if extraArgs == nil {
		extraArgs = []string{}
	}
	job, err = q.EnqueueDownloadJob(ctx, &db.EnqueueDownloadJobParams{
		URL:        rawURL,
		ArchivedBy: archivedBy,
		Refresh:    false,
		ExtraArgs:  extraArgs,
	})
	if err != nil {
		return nil, err
	}
	return &EnqueueResult{Job: job}, nil
}

// liveChannelJobReusable reports whether an existing job may be reused for a
// live channel URL (active only — not succeeded).
func liveChannelJobReusable(job *db.DownloadJob) bool {
	if job == nil {
		return false
	}
	switch job.Status {
	case "queued", "processing":
		return true
	default:
		return false
	}
}
