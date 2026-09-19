package stitch

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgtype"
)

// AlignmentJob identifies a queued forced-alignment request.
type AlignmentJob struct {
	ID           pgtype.UUID     `json:"id"`
	CaptionID    string          `json:"caption_id"`
	Status       string          `json:"status"`
	Language     string          `json:"language"`
	ModelVersion string          `json:"model_version"`
	AlignmentKey string          `json:"alignment_key"`
	Result       json.RawMessage `json:"result,omitempty"`
	Error        string          `json:"error,omitempty"`
}

// QueueAlignment marks a caption pending alignment through the normal revisioned commit.
func (s *Store) QueueAlignment(ctx context.Context, owner, project pgtype.UUID, expected int64, key string, actor Actor, captionID, lang, model string) (Result, error) {
	return s.Commit(ctx, owner, project, expected, key, actor, "Request caption alignment", []Operation{{Type: "request_alignment", TargetID: captionID, Language: lang, ModelVersion: model}})
}

// AlignmentStatus returns an owned alignment job status.
func (s *Store) AlignmentStatus(ctx context.Context, owner, project, job pgtype.UUID) (AlignmentJob, error) {
	var j AlignmentJob
	e := s.db.QueryRow(ctx, `SELECT j.id,j.caption_id,j.status,j.language,j.model_version,j.alignment_key,j.result,j.error FROM stitch_alignment_jobs j JOIN stitch_projects p ON p.id=j.project_id AND p.created_by=$2 WHERE j.id=$1 AND j.project_id=$3 AND j.owner_id=$2`, job, owner, project).Scan(&j.ID, &j.CaptionID, &j.Status, &j.Language, &j.ModelVersion, &j.AlignmentKey, &j.Result, &j.Error)
	if e != nil {
		return j, e
	}
	return j, nil
}

// AlignmentStatusByKey returns the owned job for a caption alignment fingerprint.
func (s *Store) AlignmentStatusByKey(ctx context.Context, owner, project pgtype.UUID, key string) (AlignmentJob, error) {
	var j AlignmentJob
	err := s.db.QueryRow(ctx, `SELECT j.id,j.caption_id,j.status,j.language,j.model_version,j.alignment_key,j.result,j.error FROM stitch_alignment_jobs j JOIN stitch_projects p ON p.id=j.project_id AND p.created_by=$1 WHERE j.project_id=$2 AND j.owner_id=$1 AND j.alignment_key=$3`, owner, project, key).Scan(&j.ID, &j.CaptionID, &j.Status, &j.Language, &j.ModelVersion, &j.AlignmentKey, &j.Result, &j.Error)
	return j, err
}
