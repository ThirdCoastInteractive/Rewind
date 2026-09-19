package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

const (
	metadataCatalogDepth   = 400
	metadataCatalogBatch   = 80
	subtitleFetchErrorFile = "subtitle-fetch.error"
)

func recordSubtitleFetchError(destDir string, fetchErr error) {
	message := strings.TrimSpace(fetchErr.Error())
	var execErr *ytdlp.ExecError
	if errors.As(fetchErr, &execErr) && strings.TrimSpace(execErr.Stderr) != "" {
		message = strings.TrimSpace(execErr.Stderr)
	}
	if runes := []rune(message); len(runes) > 4000 {
		message = string(runes[:4000])
	}
	if err := os.WriteFile(filepath.Join(destDir, subtitleFetchErrorFile), []byte(message), 0o600); err != nil {
		slog.Warn("failed to persist subtitle fetch error", "error", err)
	}
}

func processMetadataCatalogJob(ctx context.Context, q *db.Queries, client *ytdlp.Client, job *db.DownloadJob) error {
	jobID := uuidString(job.ID)
	slog.Info("Expanding metadata catalog", "job_id", jobID, "url", job.URL)

	entries, err := client.ListPlaylistEntries(ctx, job.URL, append(ytdlp.RateLimitArgs(), "--playlist-end", strconv.Itoa(metadataCatalogDepth))...)
	if err != nil {
		return fmt.Errorf("list catalog entries: %w", err)
	}

	_, canonicalDomain, _ := videoid.NormalizeSourceURL(job.URL)
	candidates := collectPlaylistCandidates(canonicalDomain, entries)
	states, err := q.ListCatalogCandidateStates(ctx, candidateUUIDs(candidates))
	if err != nil {
		return fmt.Errorf("list catalog candidate states: %w", err)
	}
	existing := make(map[string]*db.ListCatalogCandidateStatesRow, len(states))
	for _, state := range states {
		existing[uuidString(state.ID)] = state
	}

	urls := make([]string, 0, metadataCatalogBatch)
	seenURLs := map[string]bool{}
	for _, cand := range candidates {
		state := existing[uuidString(cand.UUID)]
		queue := state == nil || (!state.HasTranscript && state.SubtitleState != "unavailable")
		if !queue || seenURLs[cand.URL] {
			continue
		}
		seenURLs[cand.URL] = true
		urls = append(urls, cand.URL)
		if len(urls) >= metadataCatalogBatch {
			break
		}
	}

	slog.Info("Metadata catalog expanded",
		"job_id", jobID,
		"entries", len(entries),
		"queued", len(urls),
		"skipped", len(candidates)-len(urls),
	)

	if len(urls) > 0 {
		if _, err := q.EnqueueChildMetadataJobs(ctx, &db.EnqueueChildMetadataJobsParams{
			ArchivedBy:  job.ArchivedBy,
			ParentJobID: job.ID,
			Urls:        urls,
		}); err != nil {
			return fmt.Errorf("enqueue metadata children: %w", err)
		}
	}

	total := int32(len(urls))
	return q.CompletePlaylistJob(ctx, &db.CompletePlaylistJobParams{
		ID:         job.ID,
		BatchTotal: &total,
		BatchLabel: nil,
	})
}

func processMetadataJob(ctx context.Context, q *db.Queries, client *ytdlp.Client, job *db.DownloadJob, destDir string) error {
	jobID := uuidString(job.ID)
	slog.Info("Fetching metadata only", "job_id", jobID, "url", job.URL)

	infoPath := filepath.Join(destDir, "metadata.info.json")
	args := append([]string{"--no-playlist"}, ytdlp.RateLimitArgs()...)
	if err := client.DumpInfoJSON(ctx, job.URL, infoPath, args...); err != nil {
		return err
	}
	if client.LastPID > 0 {
		lastPID := int64(client.LastPID)
		_ = q.UpdateDownloadJobPID(ctx, &db.UpdateDownloadJobPIDParams{ID: job.ID, ProcessPid: &lastPID})
	}
	if err := client.WriteThumbnail(ctx, job.URL, destDir, ytdlp.RateLimitArgs()...); err != nil {
		var execErr *ytdlp.ExecError
		if errors.As(err, &execErr) {
			slog.Warn("metadata thumbnail failed", "job_id", jobID, "error", err, "stderr", execErr.Stderr)
		} else {
			slog.Warn("metadata thumbnail failed", "job_id", jobID, "error", err)
		}
	}
	if err := client.WriteSubtitles(ctx, job.URL, destDir, ytdlp.RateLimitArgs()...); err != nil {
		recordSubtitleFetchError(destDir, err)
		var execErr *ytdlp.ExecError
		if errors.As(err, &execErr) {
			slog.Warn("metadata subtitles failed", "job_id", jobID, "error", err, "stderr", execErr.Stderr)
		} else {
			slog.Warn("metadata subtitles failed", "job_id", jobID, "error", err)
		}
	}

	if err := q.MarkDownloadJobSucceeded(ctx, &db.MarkDownloadJobSucceededParams{ID: job.ID, SpoolDir: &destDir, InfoJsonPath: &infoPath}); err != nil {
		return err
	}
	_, err := q.EnqueueIngestJob(ctx, job.ID)
	return err
}
