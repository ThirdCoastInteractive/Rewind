package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

// maxPlaylistEntries bounds how many videos a single playlist/channel job fans
// out, to keep enumeration buffering and the child-insert from being unbounded
// on huge channels. Entries beyond this (the oldest, since channels list
// newest-first) are not archived; the cap is logged when hit.
const maxPlaylistEntries = 5000

// processPlaylistJob expands a "playlist" download job (a playlist or channel
// URL) into one child "video" download job per contained video, skipping videos
// that are already archived. The children flow through the normal download ->
// ingest pipeline unchanged. The parent job itself downloads nothing.
//
// Dedup is exact and pre-download: each entry's deterministic video UUID
// (videoid.VideoUUID(canonicalDomain, entryID)) is computed and checked against
// existing videos — the same UUID ingest will derive — so already-archived
// videos are never re-fetched. (Even if a check races, ingest's UPSERT on the
// deterministic id prevents duplicate rows.)
func processPlaylistJob(ctx context.Context, q *db.Queries, client *ytdlp.Client, job *db.DownloadJob) error {
	jobID := uuidString(job.ID)
	slog.Info("Expanding playlist/channel", "job_id", jobID, "url", job.URL)

	entries, err := client.ListPlaylistEntries(ctx, job.URL, "--playlist-end", strconv.Itoa(maxPlaylistEntries))
	if err != nil {
		return fmt.Errorf("list playlist entries: %w", err)
	}
	if len(entries) >= maxPlaylistEntries {
		slog.Warn("playlist capped", "job_id", jobID, "cap", maxPlaylistEntries, "url", job.URL)
	}

	// Canonical domain for deterministic video UUIDs (must match how ingest
	// resolves the domain for each downloaded child, or dedup silently misses).
	_, canonicalDomain, _ := videoid.NormalizeSourceURL(job.URL)

	// Map deterministic-UUID -> child download URL, de-duplicating within the
	// playlist itself.
	urlByID := make(map[string]string, len(entries))
	candidates := make([]pgtype.UUID, 0, len(entries))
	for _, e := range entries {
		id := strings.TrimSpace(e.ID)
		if id == "" {
			continue
		}
		childURL := childDownloadURL(canonicalDomain, e)
		if childURL == "" {
			continue
		}
		pgu := pgtype.UUID{Bytes: [16]byte(videoid.VideoUUID(canonicalDomain, id)), Valid: true}
		key := uuidString(pgu)
		if _, dup := urlByID[key]; dup {
			continue
		}
		urlByID[key] = childURL
		candidates = append(candidates, pgu)
	}

	// Drop entries whose video is already archived.
	if len(candidates) > 0 {
		existing, err := q.FilterExistingVideoIDs(ctx, candidates)
		if err != nil {
			return fmt.Errorf("filter existing videos: %w", err)
		}
		for _, pgu := range existing {
			delete(urlByID, uuidString(pgu))
		}
	}

	urls := make([]string, 0, len(urlByID))
	for _, u := range urlByID {
		urls = append(urls, u)
	}

	slog.Info("Playlist expanded",
		"job_id", jobID,
		"entries", len(entries),
		"new", len(urls),
		"already_archived", len(candidates)-len(urls),
	)

	if len(urls) > 0 {
		if _, err := q.EnqueueChildDownloadJobs(ctx, &db.EnqueueChildDownloadJobsParams{
			ArchivedBy:  job.ArchivedBy,
			ParentJobID: job.ID,
			Urls:        urls,
		}); err != nil {
			return fmt.Errorf("enqueue child jobs: %w", err)
		}
	}

	total := int32(len(urls))
	return q.CompletePlaylistJob(ctx, &db.CompletePlaylistJobParams{
		ID:         job.ID,
		BatchTotal: &total,
		BatchLabel: nil,
	})
}

// childDownloadURL picks the best URL to enqueue for a flat-playlist entry.
// yt-dlp usually supplies a full URL; for YouTube we can always reconstruct a
// canonical watch URL from the id as a fallback.
func childDownloadURL(domain string, e ytdlp.FlatEntry) string {
	if u := strings.TrimSpace(e.URL); strings.Contains(u, "://") {
		return u
	}
	if domain == "youtube.com" && strings.TrimSpace(e.ID) != "" {
		return "https://www.youtube.com/watch?v=" + strings.TrimSpace(e.ID)
	}
	return strings.TrimSpace(e.URL)
}
