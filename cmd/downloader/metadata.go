package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

const (
	metadataCatalogDepth  = 400
	metadataCatalogBatch  = 80
)

func processMetadataCatalogJob(ctx context.Context, q *db.Queries, client *ytdlp.Client, job *db.DownloadJob) error {
	jobID := uuidString(job.ID)
	slog.Info("Expanding metadata catalog", "job_id", jobID, "url", job.URL)

	entries, err := client.ListPlaylistEntries(ctx, job.URL, append(ytdlp.RateLimitArgs(), "--playlist-end", strconv.Itoa(metadataCatalogDepth))...)
	if err != nil {
		return fmt.Errorf("list catalog entries: %w", err)
	}

	_, canonicalDomain, _ := videoid.NormalizeSourceURL(job.URL)
	candidates := collectPlaylistCandidates(canonicalDomain, entries)
	existing, err := filterExistingSet(ctx, q, candidateUUIDs(candidates))
	if err != nil {
		return fmt.Errorf("filter existing videos: %w", err)
	}

	urls := make([]string, 0, metadataCatalogBatch)
	for _, cand := range candidates {
		if existing[uuidString(cand.UUID)] {
			continue
		}
		urls = append(urls, cand.URL)
		if len(urls) >= metadataCatalogBatch {
			break
		}
	}

	slog.Info("Metadata catalog expanded",
		"job_id", jobID,
		"entries", len(entries),
		"new", len(urls),
		"already_present", len(candidates)-len(urls),
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

	if err := q.MarkDownloadJobSucceeded(ctx, &db.MarkDownloadJobSucceededParams{ID: job.ID, SpoolDir: &destDir, InfoJsonPath: &infoPath}); err != nil {
		return err
	}
	_, err := q.EnqueueIngestJob(ctx, job.ID)
	return err
}
