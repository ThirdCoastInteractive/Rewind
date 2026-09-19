// Package compilation snapshots editorial plans and resumes their durable executions.
package compilation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/archival"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/events"
	"thirdcoast.systems/rewind/internal/stitch"
)

// Execute creates at most one immutable execution for the requested owned revision.
func Execute(ctx context.Context, dbc *db.DatabaseConnection, id, user pgtype.UUID, revision int32, retry bool) (*db.CompilationExecution, error) {
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	plan, err := q.LockCompilationPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	if plan.CreatedBy != user {
		return nil, fmt.Errorf("plan access denied")
	}
	if revision != plan.Revision {
		return nil, fmt.Errorf("revision conflict: current revision is %d", plan.Revision)
	}
	segments, err := q.ListCompilationPlanSegments(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("plan has no segments")
	}
	execution, err := q.CreateCompilationExecution(ctx, id)
	if err != nil {
		return nil, err
	}
	if err = q.SnapshotCompilationSegments(ctx, &db.SnapshotCompilationSegmentsParams{PlanID: id, ExecutionID: execution.ID}); err != nil {
		return nil, err
	}
	if retry && execution.Status == "failed" {
		if err = q.ClearFailedExecutionDownloads(ctx, execution.ID); err != nil {
			return nil, err
		}
		if err = q.RetryCompilationExecution(ctx, execution.ID); err != nil {
			return nil, err
		}
		execution.Status = "waiting_media"
		execution.LastError = ""
		execution.StitchJobID = pgtype.UUID{}
	}
	if err = q.PublishExecutionState(ctx, execution.ID); err != nil {
		return nil, err
	}
	return execution, tx.Commit(ctx)
}

// Run resumes pending executions on startup and on database events, with a recovery sweep.
func Run(ctx context.Context, dbc *db.DatabaseConnection) {
	media, unsubMedia := events.Default.Subscribe("download_jobs", "stitch_jobs")
	defer unsubMedia()
	changed, unsubChanged := events.Default.Subscribe("compilation_changed")
	defer unsubChanged()
	timer := time.NewTicker(30 * time.Second)
	defer timer.Stop()
	wakePending := true
	for {
		if wakePending {
			if err := dbc.Queries(ctx).WakePendingCompilations(ctx); err != nil {
				slog.Error("compilation wake", "error", err)
			}
		}
		for i := 0; i < 100; i++ {
			worked, err := Advance(ctx, dbc)
			if err != nil {
				slog.Error("compilation continuation", "error", err)
				break
			}
			if !worked {
				break
			}
		}
		wakePending = false
		select {
		case <-ctx.Done():
			return
		case <-media:
			wakePending = true
		case <-changed:
		case <-timer.C:
			wakePending = true
		}
	}
}

// Advance atomically advances one execution; concurrent coordinators skip its locked row.
func Advance(ctx context.Context, dbc *db.DatabaseConnection) (bool, error) {
	ready, err := dbc.Queries(ctx).HasClaimableCompilation(ctx)
	if err != nil {
		return false, err
	}
	if !ready {
		return false, nil
	}
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	e, err := q.ClaimCompilationExecution(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	state := &db.UpdateCompilationExecutionParams{ID: e.ID, Status: e.Status}
	if e.Status == "rendering" {
		job, err := q.GetStitchJobStatus(ctx, e.StitchJobID)
		if err != nil {
			return true, err
		}
		switch job.Status {
		case db.ExportStatusReady:
			state.Status = "complete"
		case db.ExportStatusError:
			state.Status = "failed"
			if job.LastError != nil {
				state.LastError = *job.LastError
			}
		}
	} else {
		segments, err := q.ListExecutionSegments(ctx, e.ID)
		if err != nil {
			return true, err
		}
		missing := false
		for _, s := range segments {
			if s.Media != "metadata" && s.VideoPath != nil {
				continue
			}
			missing = true
			if s.DownloadStatus != nil && *s.DownloadStatus == db.JobStatusFailed {
				state.Status = "failed"
				state.LastError = "download failed for " + s.Title
				if s.DownloadError != nil {
					state.LastError += ": " + *s.DownloadError
				}
				break
			}
			if s.DownloadJobID.Valid {
				continue
			}
			result, err := archival.EnqueueURL(ctx, q, s.Src, e.CreatedBy)
			if err != nil {
				return true, err
			}
			if err = q.LinkExecutionDownload(ctx, &db.LinkExecutionDownloadParams{ExecutionID: e.ID, Position: s.Position, DownloadJobID: result.Job.ID}); err != nil {
				return true, err
			}
		}
		if len(segments) == 0 {
			state.Status = "failed"
			state.LastError = "execution has no segments"
			missing = true
		}
		if !missing {
			raw := make([]map[string]any, 0, len(segments))
			for _, s := range segments {
				raw = append(raw, map[string]any{"type": "video", "video_id": s.VideoID.String(), "title": s.Title, "start_ts": s.StartTs, "end_ts": s.EndTs, "duration": s.EndTs - s.StartTs})
			}
			raw = withGothicTitleCard(e.Title, "", raw)
			payload, err := json.Marshal(raw)
			if err != nil {
				return true, err
			}
			// Each revision owns a project, so an old render cannot replace a newer project's output.
			emptyDoc, err := json.Marshal(stitch.Document{
				Version:  stitch.CurrentVersion,
				Title:    e.Title,
				FPS:      30,
				Width:    1920,
				Height:   1080,
				Segments: []stitch.Segment{},
				Settings: stitch.Settings{Format: "mp4", Quality: "high"},
			})
			if err != nil {
				return true, err
			}
			project, err := q.CreateStitchProject(ctx, &db.CreateStitchProjectParams{CreatedBy: e.CreatedBy, Title: e.Title, Document: emptyDoc})
			if err != nil {
				return true, err
			}
			document, err := stitch.InitializeFromLegacy(ctx, tx, e.CreatedBy, project, e.Title, "mp4", "high", payload, []byte("[]"))
			if err != nil {
				return true, err
			}
			job, err := stitch.QueueCompilationRender(ctx, tx, e.CreatedBy, project, 0, "compilation:"+project.String(), document)
			if err != nil {
				return true, err
			}
			state.Status = "rendering"
			state.StitchJobID = job
			state.StitchProjectID = project
			if err = q.NotifyStitchJob(ctx, job.String()); err != nil {
				return true, err
			}
		}
	}
	if err = q.UpdateCompilationExecution(ctx, state); err != nil {
		return true, err
	}
	if err = q.PublishExecutionState(ctx, e.ID); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}
