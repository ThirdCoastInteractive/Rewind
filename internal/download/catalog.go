package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/catalog"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

func catalogCrawlLoop(ctx context.Context, dbc *db.DatabaseConnection, clientPath string) {
	q := dbc.Queries(ctx)
	if err := q.RecoverCatalogCrawls(ctx); err != nil {
		slog.Warn("catalog crawl recovery failed", "error", err)
	}
	workerID := fmt.Sprintf("catalog-%d", time.Now().UnixNano())
	for ctx.Err() == nil {
		crawl, err := q.ClaimCatalogCrawl(ctx, workerID)
		if errors.Is(err, pgx.ErrNoRows) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
		if err != nil {
			slog.Warn("catalog claim failed", "error", err)
			time.Sleep(3 * time.Second)
			continue
		}
		client := ytdlp.New()
		client.Path = clientPath
		if err := processCatalogPage(ctx, q, client, crawl); err != nil {
			attempt := max(1, int(crawl.Attempts))
			backoff := time.Duration(math.Min(math.Pow(2, float64(attempt)), 360)) * time.Minute
			backoff += time.Duration(rand.Intn(30)) * time.Second
			retryAt := time.Now().Add(backoff)
			_ = q.SetRunningCatalogCrawlStatus(ctx, &db.SetRunningCatalogCrawlStatusParams{ID: crawl.ID, Status: "retry_wait", LastError: err.Error(), RetryAt: pgtype.Timestamptz{Time: retryAt, Valid: true}})
			slog.Warn("catalog page failed", "crawl_id", crawl.ID, "retry_at", retryAt, "error", err)
		}
	}
}

func processCatalogPage(ctx context.Context, q *db.Queries, client *ytdlp.Client, crawl *db.CatalogCrawl) error {
	pageSize := int(crawl.PageSize)
	if pageSize <= 0 {
		pageSize = catalog.DefaultPageSize
	}
	next := int(crawl.NextPageIndex)
	if next < 1 {
		next = 1
	}
	start, end := catalog.PageRange(next, pageSize, int(crawl.OverlapSize))
	delay := time.Duration(500+rand.Intn(1500)) * time.Millisecond
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
	}
	args := append(ytdlp.RateLimitArgs(), "--playlist-start", strconv.Itoa(start), "--playlist-end", strconv.Itoa(end))
	entries, err := client.ListPlaylistEntries(ctx, crawl.FeedURL, args...)
	if err != nil {
		return fmt.Errorf("enumerate %s page %d: %w", crawl.FeedKind, next, err)
	}
	if len(entries) == 0 {
		return q.SetRunningCatalogCrawlStatus(ctx, &db.SetRunningCatalogCrawlStatusParams{ID: crawl.ID, Status: "complete", LastError: ""})
	}
	_, domain, _ := videoid.NormalizeSourceURL(crawl.FeedURL)
	candidates := collectPlaylistCandidates(domain, entries)
	ids := candidateUUIDs(candidates)
	states, err := q.ListCatalogCandidateStates(ctx, ids)
	if err != nil {
		return err
	}
	existing := make(map[string]*db.ListCatalogCandidateStatesRow, len(states))
	for _, state := range states {
		existing[uuidString(state.ID)] = state
	}
	urls := make([]string, 0, len(candidates))
	added, updated := int64(0), int64(0)
	seenURLs := map[string]bool{}
	for _, candidate := range candidates {
		state := existing[uuidString(candidate.UUID)]
		queue := crawl.Refresh || state == nil || (!state.HasTranscript && state.SubtitleState != "unavailable")
		if !queue || seenURLs[candidate.URL] {
			continue
		}
		seenURLs[candidate.URL] = true
		urls = append(urls, candidate.URL)
		if state == nil {
			added++
		} else {
			updated++
		}
	}
	if len(urls) > 0 {
		if _, err := q.EnqueueCatalogMetadataJobs(ctx, &db.EnqueueCatalogMetadataJobsParams{ArchivedBy: crawl.RequestedBy, Urls: urls}); err != nil {
			return err
		}
	}
	if err := q.AdvanceCatalogCrawl(ctx, &db.AdvanceCatalogCrawlParams{ID: crawl.ID, NextPageIndex: int32(next + pageSize), EntriesSeen: int64(len(candidates)), EntriesAdded: added, EntriesUpdated: updated}); err != nil {
		return err
	}
	status := "queued"
	if len(entries) < pageSize {
		status = "complete"
	}
	if err := q.SetRunningCatalogCrawlStatus(ctx, &db.SetRunningCatalogCrawlStatusParams{ID: crawl.ID, Status: status, LastError: ""}); err != nil {
		return err
	}
	slog.Info("catalog page complete", "crawl_id", crawl.ID, "feed", crawl.FeedKind, "start", start, "end", end, "seen", len(candidates), "queued", len(urls), "status", status)
	return nil
}

func subtitleBackfillLoop(ctx context.Context, dbc *db.DatabaseConnection) {
	q := dbc.Queries(ctx)
	run := func() {
		count, err := q.EnqueueSubtitleBackfillJobs(ctx, 50)
		if err != nil {
			slog.Warn("subtitle backfill enqueue failed", "error", err)
			return
		}
		if count > 0 {
			slog.Info("queued subtitle backfill", "count", count)
		}
	}
	run()
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
