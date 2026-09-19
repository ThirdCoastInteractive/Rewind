package stitch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

// RenderOptions describes a validated immutable render request.
type RenderOptions struct {
	Format         string  `json:"format"`
	Quality        string  `json:"quality"`
	CaptionMode    string  `json:"caption_mode"`
	Scope          string  `json:"scope"`
	StartUS        int64   `json:"start_us"`
	EndUS          int64   `json:"end_us"`
	FrameTimeUS    int64   `json:"frame_time_us"`
	WordHighlight  bool    `json:"word_highlight"`
	LoudnessTarget float64 `json:"loudness_target,omitempty"`
}

// RenderJob is an immutable queued render request.
type RenderJob struct {
	ID          pgtype.UUID       `json:"id"`
	ProjectID   pgtype.UUID       `json:"project_id"`
	Revision    int64             `json:"project_revision"`
	Kind        string            `json:"kind"`
	Status      string            `json:"status"`
	Options     RenderOptions     `json:"options"`
	Document    json.RawMessage   `json:"document_snapshot"`
	RequestHash string            `json:"request_hash"`
	FilePath    string            `json:"file_path,omitempty"`
	AssetURL    string            `json:"asset_url,omitempty"`
	SidecarURLs map[string]string `json:"sidecar_urls,omitempty"`
	MIME        string            `json:"mime,omitempty"`
	Progress    int32             `json:"progress_pct"`
	Error       string            `json:"error,omitempty"`
}

func validateRenderOptions(o RenderOptions, d Document) error {
	if o.Format != "mp4" && o.Format != "webm" {
		return fmt.Errorf("format must be mp4 or webm")
	}
	if o.Quality != "high" && o.Quality != "max" {
		return fmt.Errorf("quality must be high or max")
	}
	if o.Scope != "all" && o.Scope != "range" {
		return fmt.Errorf("scope must be all or range")
	}
	switch o.CaptionMode {
	case "none", "burn", "srt", "vtt":
	default:
		return fmt.Errorf("invalid caption mode")
	}
	if o.Scope == "range" && (o.StartUS < 0 || o.EndUS <= o.StartUS) {
		return fmt.Errorf("invalid render range")
	}
	if o.FrameTimeUS < 0 {
		return fmt.Errorf("invalid frame time")
	}
	if o.FrameTimeUS > 0 && o.FrameTimeUS >= documentDuration(d) {
		return fmt.Errorf("frame time exceeds document")
	}
	if o.WordHighlight && (o.CaptionMode != "burn" || len(d.Captions) == 0) {
		return fmt.Errorf("word highlight requires burn captions with aligned words")
	}
	if o.WordHighlight {
		for _, c := range d.Captions {
			if c.Style.WordHighlight && (c.Alignment != "valid" || len(c.Words) == 0) {
				return fmt.Errorf("caption %q has pending word alignment", c.ID)
			}
		}
	}
	for _, c := range d.Captions {
		if o.CaptionMode == "burn" && c.Style.WordHighlight && (c.Alignment != "valid" || len(c.Words) == 0) {
			return fmt.Errorf("caption %q has pending word alignment", c.ID)
		}
	}
	if o.LoudnessTarget != 0 && (math.IsNaN(o.LoudnessTarget) || math.IsInf(o.LoudnessTarget, 0) || o.LoudnessTarget < -70 || o.LoudnessTarget > -5) {
		return fmt.Errorf("invalid loudness target")
	}
	if o.Scope == "range" && o.EndUS > documentDuration(d) {
		return fmt.Errorf("render range exceeds document")
	}
	return nil
}
func documentDuration(d Document) int64 {
	var end int64
	for _, s := range d.Segments {
		if s.StartUS+s.DurationUS > end {
			end = s.StartUS + s.DurationUS
		}
	}
	return end
}

// QueueExport validates and queues an immutable snapshot without editing the project.
func (s *Store) QueueExport(ctx context.Context, owner, project pgtype.UUID, revision int64, key string, o RenderOptions) (RenderJob, error) {
	return s.queueRender(ctx, owner, project, revision, key, "export", o)
}

// QueuePreview queues a bounded timeline preview using the same immutable snapshot.
func (s *Store) QueuePreview(ctx context.Context, owner, project pgtype.UUID, revision int64, key string, o RenderOptions) (RenderJob, error) {
	if o.Scope != "range" || o.EndUS <= o.StartUS || o.EndUS-o.StartUS > 15_000_000 {
		return RenderJob{}, fmt.Errorf("preview range must be between 0 and 15 seconds")
	}
	return s.queueRender(ctx, owner, project, revision, key, "preview", o)
}

// QueueFrame queues a single frame at an explicit document timestamp.
func (s *Store) QueueFrame(ctx context.Context, owner, project pgtype.UUID, revision int64, key string, o RenderOptions) (RenderJob, error) {
	if o.FrameTimeUS < 0 {
		return RenderJob{}, fmt.Errorf("frame timestamp is required")
	}
	o.Scope = "range"
	o.StartUS = o.FrameTimeUS
	o.EndUS = o.FrameTimeUS + 1
	return s.queueRender(ctx, owner, project, revision, key, "frame", o)
}

func (s *Store) queueRender(ctx context.Context, owner, project pgtype.UUID, revision int64, key, kind string, o RenderOptions) (RenderJob, error) {
	if revision < 0 || strings.TrimSpace(key) == "" {
		return RenderJob{}, fmt.Errorf("revision and operation key are required")
	}
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return RenderJob{}, e
	}
	defer tx.Rollback(ctx)
	snap, _, _, e := s.snapshot(ctx, tx, owner, project, true)
	if e != nil {
		return RenderJob{}, e
	}
	var job RenderJob
	optionsJSON, _ := json.Marshal(o)
	opts, _ := json.Marshal(struct {
		Revision int64         `json:"revision"`
		Kind     string        `json:"kind"`
		Options  RenderOptions `json:"options"`
	}{revision, kind, o})
	sum := sha256.Sum256(opts)
	requestHash := hex.EncodeToString(sum[:])
	e = tx.QueryRow(ctx, `SELECT id,project_revision,render_kind,status,document_snapshot,render_request_hash,render_options FROM stitch_jobs WHERE project_id=$1 AND export_operation_key=$2`, project, key).Scan(&job.ID, &job.Revision, &job.Kind, &job.Status, &job.Document, &job.RequestHash, &opts)
	if e == nil {
		if job.RequestHash != requestHash {
			return RenderJob{}, ErrIdempotency
		}
		_ = json.Unmarshal(opts, &job.Options)
		job.ProjectID = project
		return job, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return RenderJob{}, e
	}
	// Different operation keys must not enqueue a second in-flight export for
	// the same project title + kind (double-click / agent retry storms).
	e = tx.QueryRow(ctx, `SELECT id,project_revision,render_kind,status,document_snapshot,render_request_hash,render_options FROM stitch_jobs WHERE project_id=$1 AND title=$2 AND render_kind=$3 AND status IN ('queued','processing') ORDER BY created_at ASC LIMIT 1 FOR UPDATE`, project, snap.Document.Title, kind).Scan(&job.ID, &job.Revision, &job.Kind, &job.Status, &job.Document, &job.RequestHash, &opts)
	if e == nil {
		_ = json.Unmarshal(opts, &job.Options)
		job.ProjectID = project
		return job, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return RenderJob{}, e
	}
	if snap.Revision != revision {
		return RenderJob{}, &ConflictError{CurrentRevision: snap.Revision}
	}
	if !snap.Enabled {
		return RenderJob{}, ErrNotFound
	}
	if e = validateRenderOptions(o, snap.Document); e != nil {
		return RenderJob{}, e
	}
	envelope, e := BuildRenderSnapshotWithAssets(ctx, tx, owner, project, snap.Document, o)
	if e != nil {
		return RenderJob{}, e
	}
	doc, _ := json.Marshal(envelope)
	e = tx.QueryRow(ctx, `INSERT INTO stitch_jobs(created_by,title,format,quality,segments,global_filters,project_id,export_operation_key,render_kind,project_revision,document_snapshot,render_options,render_request_hash,range_start_us,range_end_us,frame_time_us) VALUES($1,$2,$3,$4,$5,'[]',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id,status`, owner, snap.Document.Title, o.Format, o.Quality, []byte("[]"), project, key, kind, revision, doc, optionsJSON, requestHash, o.StartUS, o.EndUS, o.FrameTimeUS).Scan(&job.ID, &job.Status)
	if e != nil {
		return RenderJob{}, e
	}
	job.ProjectID = project
	job.Revision = revision
	job.Kind = kind
	job.Options = o
	job.Document = doc
	job.RequestHash = requestHash
	if e = tx.Commit(ctx); e != nil {
		return RenderJob{}, e
	}
	_, _ = s.db.Exec(ctx, "SELECT pg_notify('stitch_jobs', $1)", job.ID.String())
	return job, nil
}

// ListRenderJobs returns recent render jobs for an owned project.
func (s *Store) ListRenderJobs(ctx context.Context, owner, project pgtype.UUID) ([]RenderJob, error) {
	rows, err := s.db.Query(ctx, `SELECT id FROM stitch_jobs WHERE project_id=$1 AND created_by=$2 ORDER BY created_at DESC LIMIT 20`, project, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]RenderJob, 0, len(ids))
	for _, id := range ids {
		job, err := s.GetRenderJob(ctx, owner, id)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, nil
}

// GetRenderJob returns a render job only when it belongs to the owner.
func (s *Store) GetRenderJob(ctx context.Context, owner, id pgtype.UUID) (RenderJob, error) {
	var j RenderJob
	var doc []byte
	var format, quality string
	var project pgtype.UUID
	var opts RenderOptions
	var filePath, lastError *string
	var progress *int32
	var optsRaw []byte
	var revision *int64
	var requestHash *string
	if e := s.db.QueryRow(ctx, `SELECT id,project_id,project_revision,COALESCE(render_kind, 'export'),status,document_snapshot,format,quality,COALESCE(render_options, '{}'::jsonb),render_request_hash,file_path,progress_pct,last_error FROM stitch_jobs WHERE id=$1 AND created_by=$2`, id, owner).Scan(&j.ID, &project, &revision, &j.Kind, &j.Status, &doc, &format, &quality, &optsRaw, &requestHash, &filePath, &progress, &lastError); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return RenderJob{}, ErrNotFound
		}
		return RenderJob{}, e
	}
	j.ProjectID = project
	if revision != nil {
		j.Revision = *revision
	}
	if requestHash != nil {
		j.RequestHash = *requestHash
	}
	j.Document = doc
	j.Options = opts
	_ = json.Unmarshal(optsRaw, &j.Options)
	j.Options.Format = format
	j.Options.Quality = quality
	if filePath != nil {
		j.FilePath = *filePath
		if j.Status == "ready" {
			j.AssetURL = "/api/stitch/" + j.ID.String() + "/stream"
		}
	}
	if progress != nil {
		j.Progress = *progress
	}
	if lastError != nil {
		j.Error = *lastError
	}
	if j.Kind == "frame" {
		j.MIME = "image/png"
	} else if format == "webm" {
		j.MIME = "video/webm"
	} else {
		j.MIME = "video/mp4"
	}
	j.SidecarURLs = renderSidecarURLs(j.ID, j.Status, j.Options.CaptionMode)
	return j, nil
}

func renderSidecarURLs(id pgtype.UUID, status, mode string) map[string]string {
	if status != "ready" || (mode != "srt" && mode != "vtt") {
		return nil
	}
	return map[string]string{mode: "/api/stitch/" + id.String() + "/captions?format=" + mode}
}

var _ = db.DatabaseConnection{}
