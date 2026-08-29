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
func processPlaylistJob(ctx context.Context, dbc *db.DatabaseConnection, q *db.Queries, client *ytdlp.Client, job *db.DownloadJob) error {
	// Scan jobs created by channel watching carry a watch_id and get
	// seen-ledger-aware expansion instead of plain archived-video dedup.
	if job.WatchID.Valid {
		return processWatchScanJob(ctx, dbc, q, client, job)
	}

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

	candidates := collectPlaylistCandidates(canonicalDomain, entries)

	// Drop entries whose video is already archived.
	existing, err := filterExistingSet(ctx, q, candidateUUIDs(candidates))
	if err != nil {
		return fmt.Errorf("filter existing videos: %w", err)
	}

	urls := make([]string, 0, len(candidates))
	for _, cand := range candidates {
		if !existing[uuidString(cand.UUID)] {
			urls = append(urls, cand.URL)
		}
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

// playlistCandidate is one enumerated entry paired with its deterministic
// video UUID and the URL a child download job should use.
type playlistCandidate struct {
	UUID  pgtype.UUID
	URL   string
	Entry ytdlp.FlatEntry
}

// collectPlaylistCandidates maps flat entries to download candidates,
// de-duplicating within the listing itself. Entries without a usable id or
// URL are dropped.
func collectPlaylistCandidates(canonicalDomain string, entries []ytdlp.FlatEntry) []playlistCandidate {
	seen := make(map[string]bool, len(entries))
	out := make([]playlistCandidate, 0, len(entries))
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
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, playlistCandidate{UUID: pgu, URL: childURL, Entry: e})
	}
	return out
}

// candidateUUIDs extracts the deterministic UUIDs from candidates.
func candidateUUIDs(cands []playlistCandidate) []pgtype.UUID {
	ids := make([]pgtype.UUID, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.UUID)
	}
	return ids
}

// filterExistingSet returns the subset of ids that already exist as archived
// videos, as a set keyed by UUID string. Empty input skips the query.
func filterExistingSet(ctx context.Context, q *db.Queries, ids []pgtype.UUID) (map[string]bool, error) {
	set := make(map[string]bool)
	if len(ids) == 0 {
		return set, nil
	}
	existing, err := q.FilterExistingVideoIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, pgu := range existing {
		set[uuidString(pgu)] = true
	}
	return set, nil
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
