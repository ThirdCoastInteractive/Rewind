package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/cronspec"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

// errWatchDeleted marks a scan aborted because its watch row disappeared
// mid-scan; the scan job completes empty instead of failing.
var errWatchDeleted = errors.New("watch deleted")

const (
	// watchSchedulerInterval is how often each downloader replica checks for
	// due watches. Claiming is SKIP LOCKED, so replicas never double-schedule.
	watchSchedulerInterval = 15 * time.Second

	// watchClaimBatch bounds how many due watches one tick schedules.
	watchClaimBatch = 10

	// watchScanDepth is how many of the newest entries (per channel tab) a
	// routine scan enumerates. New uploads appear at the head of a channel
	// listing, so a shallow scan finds them without walking the full catalog.
	watchScanDepth = 100
)

// watchSchedulerLoop periodically turns due watched channels into scan jobs
// (playlist-kind download jobs tagged with the watch id). The jobs are picked
// up by the normal download workers, so scanning shares their yt-dlp logging,
// retry, and cookie machinery.
func watchSchedulerLoop(ctx context.Context, dbc *db.DatabaseConnection) {
	slog.Info("Watch scheduler started", "interval", watchSchedulerInterval.String())
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(watchSchedulerInterval):
		}

		if err := scheduleDueWatches(ctx, dbc); err != nil {
			slog.Error("watch scheduler tick failed", "error", err)
		}
	}
}

// scheduleDueWatches claims due watches, advances their next_scan_at to the
// next cron occurrence, and enqueues one scan job each — atomically, so a
// crash mid-tick never leaves a watch half-scheduled.
func scheduleDueWatches(ctx context.Context, dbc *db.DatabaseConnection) error {
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	watches, err := q.ClaimDueWatchedChannels(ctx, watchClaimBatch)
	if err != nil {
		return err
	}

	for _, w := range watches {
		next, err := cronspec.Next(w.CronSchedule, time.Now())
		if err != nil {
			slog.Error("invalid cron schedule on watch; retrying in 1h",
				"watch_id", uuidString(w.ID), "schedule", w.CronSchedule, "error", err)
			next = time.Now().Add(time.Hour)
		}

		if err := q.ScheduleWatchedChannelNextScan(ctx, &db.ScheduleWatchedChannelNextScanParams{
			ID:         w.ID,
			NextScanAt: pgtype.Timestamptz{Time: next, Valid: true},
		}); err != nil {
			return err
		}

		job, err := q.EnqueueWatchScanJob(ctx, &db.EnqueueWatchScanJobParams{
			URL:        w.URL,
			ArchivedBy: w.CreatedBy,
			WatchID:    w.ID,
		})
		if err != nil {
			return err
		}

		slog.Info("Scheduled watch scan",
			"watch_id", uuidString(w.ID),
			"job_id", uuidString(job.ID),
			"url", w.URL,
			"next_scan_at", next.Format(time.RFC3339),
		)
	}

	return tx.Commit(ctx)
}

// processWatchScanJob expands a channel-watch scan job. Like a playlist job it
// fans out into child video jobs, but dedup goes through the watch's
// seen-ledger so pending/failed downloads are never re-enqueued by later
// scans, and non-backfill watches only archive videos observed after the
// watch was created. Scan outcome is recorded on the watch either way.
func processWatchScanJob(ctx context.Context, dbc *db.DatabaseConnection, q *db.Queries, client *ytdlp.Client, job *db.DownloadJob) error {
	watch, err := q.GetWatchedChannel(ctx, job.WatchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			zero := int32(0)
			label := "Channel watch (deleted)"
			_ = q.CompletePlaylistJob(ctx, &db.CompletePlaylistJobParams{ID: job.ID, BatchTotal: &zero, BatchLabel: &label})
			_ = q.ArchiveJob(ctx, job.ID)
			return nil
		}
		return fmt.Errorf("load watch: %w", err)
	}

	if err := runWatchScan(ctx, dbc, q, client, job, watch); err != nil {
		if db.IsForeignKeyViolationErr(err) || errors.Is(err, errWatchDeleted) {
			slog.Info("watch deleted mid-scan; completing scan job empty",
				"job_id", uuidString(job.ID), "watch_id", uuidString(watch.ID))
			zero := int32(0)
			label := "Channel watch (deleted)"
			_ = q.CompletePlaylistJob(ctx, &db.CompletePlaylistJobParams{ID: job.ID, BatchTotal: &zero, BatchLabel: &label})
			_ = q.ArchiveJob(ctx, job.ID)
			return nil
		}

		msg := err.Error()
		status := "error"
		_ = q.RecordWatchedChannelScanResult(ctx, &db.RecordWatchedChannelScanResultParams{
			ID:            watch.ID,
			Status:        &status,
			LastError:     &msg,
			Found:         0,
			MarkFirstDone: false,
		})
		return err
	}
	return nil
}

func runWatchScan(ctx context.Context, dbc *db.DatabaseConnection, q *db.Queries, client *ytdlp.Client, job *db.DownloadJob, watch *db.WatchedChannel) error {
	jobID := uuidString(job.ID)
	firstScan := !watch.FirstScanDone
	deep := firstScan
	limit := watchScanDepth
	if deep {
		limit = maxPlaylistEntries
	}

	slog.Info("Scanning watched channel",
		"job_id", jobID,
		"watch_id", uuidString(watch.ID),
		"url", watch.URL,
		"first_scan", firstScan,
		"deep", deep,
	)

	listing, err := client.ListChannelVideos(ctx, watch.URL, limit)
	if err != nil {
		return fmt.Errorf("enumerate channel: %w", err)
	}

	if title := channelTitleFromListing(listing.Title); strings.TrimSpace(watch.Label) == "" && title != "" {
		if err := q.SetWatchedChannelLabelIfEmpty(ctx, &db.SetWatchedChannelLabelIfEmptyParams{
			ID:    watch.ID,
			Label: title,
		}); err == nil {
			watch.Label = title
		}
	}

	_, canonicalDomain, _ := videoid.NormalizeSourceURL(watch.URL)

	candidates := collectPlaylistCandidates(canonicalDomain, listing.Entries)
	fresh, alreadyArchived, err := filterWatchFresh(ctx, q, watch.ID, candidates)
	if err != nil {
		return err
	}

	if !deep && !firstScan &&
		len(candidates) >= watchScanDepth && len(fresh) == len(candidates) {
		slog.Warn("watch head window fully unseen; escalating to deep scan",
			"job_id", jobID, "watch_id", uuidString(watch.ID), "head", len(candidates))

		listing, err = client.ListChannelVideos(ctx, watch.URL, maxPlaylistEntries)
		if err != nil {
			return fmt.Errorf("deep enumerate channel: %w", err)
		}
		candidates = collectPlaylistCandidates(canonicalDomain, listing.Entries)
		fresh, alreadyArchived, err = filterWatchFresh(ctx, q, watch.ID, candidates)
		if err != nil {
			return err
		}
	}

	enqueue := !(firstScan && !watch.Backfill)
	found := int32(0)
	if enqueue {
		found = int32(len(fresh))
	}

	qtx, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return fmt.Errorf("begin scan write tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := qtx.GetWatchedChannel(ctx, watch.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("watch vanished before write: %w", errWatchDeleted)
		}
		return fmt.Errorf("re-check watch: %w", err)
	}

	if err := recordWatchSeen(ctx, qtx, watch.ID, fresh, enqueue); err != nil {
		return err
	}
	if err := recordWatchSeen(ctx, qtx, watch.ID, alreadyArchived, false); err != nil {
		return err
	}

	if enqueue && len(fresh) > 0 {
		urls := make([]string, 0, len(fresh))
		for _, c := range fresh {
			urls = append(urls, c.URL)
		}
		if _, err := qtx.EnqueueChildDownloadJobs(ctx, &db.EnqueueChildDownloadJobsParams{
			ArchivedBy:  job.ArchivedBy,
			ParentJobID: job.ID,
			Urls:        urls,
		}); err != nil {
			return fmt.Errorf("enqueue child jobs: %w", err)
		}
	}

	label := "Watch: " + watchDisplayName(watch)
	if err := qtx.CompletePlaylistJob(ctx, &db.CompletePlaylistJobParams{
		ID:         job.ID,
		BatchTotal: &found,
		BatchLabel: &label,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit scan write tx: %w", err)
	}

	if found == 0 {
		_ = q.ArchiveJob(ctx, job.ID)
	}

	status := "ok"
	if err := q.RecordWatchedChannelScanResult(ctx, &db.RecordWatchedChannelScanResultParams{
		ID:            watch.ID,
		Status:        &status,
		LastError:     nil,
		Found:         found,
		MarkFirstDone: true,
	}); err != nil {
		return err
	}

	slog.Info("Watch scan complete",
		"job_id", jobID,
		"watch_id", uuidString(watch.ID),
		"enumerated", len(listing.Entries),
		"new_enqueued", found,
		"seeded_only", !enqueue,
		"already_archived", len(alreadyArchived),
	)
	return nil
}

// filterWatchFresh splits candidates into fresh (never seen by this watch,
// not archived) and alreadyArchived (not in the seen-ledger but present in
// videos via some other path). Seen-ledger hits are dropped entirely.
func filterWatchFresh(ctx context.Context, q *db.Queries, watchID pgtype.UUID, candidates []playlistCandidate) (fresh, alreadyArchived []playlistCandidate, err error) {
	if len(candidates) == 0 {
		return nil, nil, nil
	}

	seenRows, err := q.FilterSeenWatchedChannelVideos(ctx, &db.FilterSeenWatchedChannelVideosParams{
		WatchID: watchID,
		Ids:     candidateUUIDs(candidates),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("filter seen videos: %w", err)
	}
	seen := make(map[string]bool, len(seenRows))
	for _, pgu := range seenRows {
		seen[uuidString(pgu)] = true
	}

	unseen := make([]playlistCandidate, 0, len(candidates))
	for _, c := range candidates {
		if !seen[uuidString(c.UUID)] {
			unseen = append(unseen, c)
		}
	}

	existing, err := filterExistingSet(ctx, q, candidateUUIDs(unseen))
	if err != nil {
		return nil, nil, fmt.Errorf("filter existing videos: %w", err)
	}

	for _, c := range unseen {
		if existing[uuidString(c.UUID)] {
			alreadyArchived = append(alreadyArchived, c)
		} else {
			fresh = append(fresh, c)
		}
	}
	return fresh, alreadyArchived, nil
}

// recordWatchSeen writes candidates into the watch's seen-ledger in one pgx
// batch. Conflicts (concurrently recorded rows) are ignored by the query.
func recordWatchSeen(ctx context.Context, q *db.Queries, watchID pgtype.UUID, cands []playlistCandidate, enqueued bool) error {
	if len(cands) == 0 {
		return nil
	}
	params := make([]*db.RecordWatchedChannelVideosParams, 0, len(cands))
	for _, c := range cands {
		params = append(params, &db.RecordWatchedChannelVideosParams{
			WatchID:  watchID,
			VideoID:  c.UUID,
			EntryID:  strings.TrimSpace(c.Entry.ID),
			URL:      c.URL,
			Title:    c.Entry.Title,
			Enqueued: enqueued,
		})
	}

	br := q.RecordWatchedChannelVideos(ctx, params)
	var batchErr error
	br.Exec(func(_ int, err error) {
		if err != nil && batchErr == nil {
			batchErr = err
		}
	})
	if err := br.Close(); err != nil && batchErr == nil {
		batchErr = err
	}
	if batchErr != nil {
		return fmt.Errorf("record seen videos: %w", batchErr)
	}
	return nil
}

// channelTitleFromListing strips yt-dlp's tab suffix ("X - Videos") from a
// listing title: single-tab channels short-circuit to the tab playlist, whose
// title carries the suffix, but the label should be the channel name.
func channelTitleFromListing(title string) string {
	title = strings.TrimSpace(title)
	for _, suf := range []string{" - Videos", " - Shorts", " - Live"} {
		if strings.HasSuffix(title, suf) {
			return strings.TrimSuffix(title, suf)
		}
	}
	return title
}

// watchDisplayName returns the watch's label, falling back to its URL.
func watchDisplayName(watch *db.WatchedChannel) string {
	if label := strings.TrimSpace(watch.Label); label != "" {
		return label
	}
	return watch.URL
}
