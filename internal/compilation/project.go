package compilation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

// Project creates one editable stitch snapshot per owned plan revision, without downloads or rendering.
// Repeated calls return the existing project and preserve subsequent human edits.
func Project(ctx context.Context, dbc *db.DatabaseConnection, id, user pgtype.UUID, revision int32) (pgtype.UUID, error) {
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return pgtype.UUID{}, err
	}
	defer tx.Rollback(ctx)
	plan, err := q.LockCompilationPlan(ctx, id)
	if err != nil {
		return pgtype.UUID{}, err
	}
	if plan.CreatedBy != user {
		return pgtype.UUID{}, fmt.Errorf("plan access denied")
	}
	if revision != plan.Revision {
		return pgtype.UUID{}, fmt.Errorf("revision conflict: current revision is %d", plan.Revision)
	}
	project := pgtype.UUID{Bytes: uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("rewind://compilation/%s/revision/%d/stitch", id.String(), revision))), Valid: true}
	n, err := q.CreateCompilationStitchProject(ctx, &db.CreateCompilationStitchProjectParams{ID: project, CreatedBy: user, Title: plan.Title})
	if err != nil {
		return pgtype.UUID{}, err
	}
	if n == 0 {
		existing, err := q.GetStitchProject(ctx, project)
		if err != nil {
			return pgtype.UUID{}, err
		}
		if existing.CreatedBy != user {
			return pgtype.UUID{}, fmt.Errorf("project access denied")
		}
		return project, tx.Commit(ctx)
	}
	segments, err := q.ListCompilationPlanSegments(ctx, id)
	if err != nil {
		return pgtype.UUID{}, err
	}
	if len(segments) == 0 {
		return pgtype.UUID{}, fmt.Errorf("plan has no segments")
	}
	raw := make([]map[string]any, 0, len(segments))
	for _, s := range segments {
		title := strings.TrimSpace(s.SelectionRationale)
		if title == "" {
			title = s.VideoTitle
		}
		if len(title) > 80 {
			title = title[:80]
		}
		clip, err := q.CreateClip(ctx, &db.CreateClipParams{
			VideoID:     s.VideoID,
			StartTs:     s.StartTs,
			EndTs:       s.EndTs,
			Duration:    s.EndTs - s.StartTs,
			Title:       title,
			Description: s.SelectionRationale,
			Color:       "#6ea8fe",
			Tags:        []byte("[]"),
			CreatedBy:   user,
		})
		if err != nil {
			return pgtype.UUID{}, err
		}
		raw = append(raw, map[string]any{
			"type":     "clip",
			"clip_id":  clip.ID.String(),
			"video_id": s.VideoID.String(),
			"title":    title,
			"start_ts": s.StartTs,
			"end_ts":   s.EndTs,
			"duration": s.EndTs - s.StartTs,
		})
	}
	subtitle := ""
	if plan.CreatorID.Valid {
		if creator, err := q.GetCreator(ctx, plan.CreatorID); err == nil && creator != nil {
			subtitle = strings.TrimSpace(creator.Name)
		}
	}
	raw = withGothicTitleCard(plan.Title, subtitle, raw)
	payload, err := json.Marshal(raw)
	if err != nil {
		return pgtype.UUID{}, err
	}
	if _, err = stitch.InitializeFromLegacy(ctx, tx, user, project, plan.Title, "mp4", "high", payload, []byte("[]")); err != nil {
		return pgtype.UUID{}, fmt.Errorf("initialize canonical stitch document: %w", err)
	}
	return project, tx.Commit(ctx)
}
