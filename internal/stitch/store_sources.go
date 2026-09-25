package stitch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/pkg/plugin"
)

type workspaceResolver interface {
	WorkspaceForUser(context.Context, string) (string, error)
}

// workspaceTenant is intentionally resolved at the store boundary. HTTP
// preflight is insufficient because MCP and other callers enter the store
// directly. OSS keeps its existing owner checks; Live must have a trusted
// workspace scope or source validation fails closed.
func workspaceTenant(ctx context.Context, owner pgtype.UUID) (pgtype.UUID, bool, error) {
	if plugin.LiveIngest() == nil {
		return pgtype.UUID{}, false, nil
	}
	a := plugin.Auth()
	r, ok := a.(workspaceResolver)
	if !ok {
		return pgtype.UUID{}, true, fmt.Errorf("live workspace resolver unavailable")
	}
	raw, err := r.WorkspaceForUser(ctx, owner.String())
	if err != nil || strings.TrimSpace(raw) == "" {
		return pgtype.UUID{}, true, fmt.Errorf("live workspace unavailable")
	}
	tenant, err := parseUUID(raw)
	var zero [16]byte
	if err != nil || !tenant.Valid || tenant.Bytes == zero {
		return pgtype.UUID{}, true, fmt.Errorf("live workspace is invalid")
	}
	return tenant, true, nil
}

func sameWorkspaceCreator(ctx context.Context, owner pgtype.UUID, creator string) bool {
	tenant, enforce, err := workspaceTenant(ctx, owner)
	if err != nil || !enforce {
		return false
	}
	a := plugin.Auth()
	r, ok := a.(workspaceResolver)
	if !ok {
		return false
	}
	raw, err := r.WorkspaceForUser(ctx, creator)
	other, scanErr := parseUUID(raw)
	return err == nil && scanErr == nil && other.Valid && other.Bytes == tenant.Bytes
}

type frozenClip struct {
	ClipID      string          `json:"clip_id"`
	VideoID     string          `json:"video_id"`
	StartUS     int64           `json:"start_us"`
	DurationUS  int64           `json:"duration_us"`
	EndUS       float64         `json:"end_ts"`
	Crops       json.RawMessage `json:"crops"`
	ShotList    json.RawMessage `json:"shot_list,omitempty"`
	FilterStack json.RawMessage `json:"filter_stack,omitempty"`
}

func hydrateLegacySegments(ctx context.Context, tx pgx.Tx, owner pgtype.UUID, raw json.RawMessage) (json.RawMessage, error) {
	tenant, enforceTenant, err := workspaceTenant(ctx, owner)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &rows) != nil {
		return raw, nil
	}
	for _, row := range rows {
		cid, _ := row["clip_id"].(string)
		if cid == "" {
			continue
		}
		id, err := parseUUID(cid)
		if err != nil {
			return nil, err
		}
		var start, end, dur float64
		var vid pgtype.UUID
		var videoTenant pgtype.UUID
		var crops, shotList, filters []byte
		clipQuery := `SELECT c.video_id,c.start_ts,c.end_ts,c.duration,c.crops,c.shot_list,c.filter_stack,v.tenant_id FROM clips c JOIN videos v ON v.id=c.video_id WHERE c.id=$1`
		clipArgs := []any{id}
		if !enforceTenant {
			clipQuery += ` AND c.created_by=$2`
			clipArgs = append(clipArgs, owner)
		}
		if err = tx.QueryRow(ctx, clipQuery, clipArgs...).Scan(&vid, &start, &end, &dur, &crops, &shotList, &filters, &videoTenant); err != nil {
			return nil, err
		}
		if enforceTenant && videoTenant.Bytes != tenant.Bytes {
			return nil, fmt.Errorf("clip %s source is outside workspace", cid)
		}
		if _, ok := row["start_ts"]; !ok {
			row["start_ts"] = start
		}
		if _, ok := row["end_ts"]; !ok {
			row["end_ts"] = end
		}
		meta, _ := json.Marshal(frozenClip{ClipID: cid, VideoID: vid.String(), StartUS: int64(start * 1e6), DurationUS: int64(dur * 1e6), EndUS: end, Crops: crops, ShotList: shotList, FilterStack: filters})
		row["legacy_metadata"] = json.RawMessage(meta)
		if _, ok := row["start"]; !ok {
			row["start"] = start
		}
		if _, ok := row["duration"]; !ok {
			row["duration"] = dur
		}
		if _, ok := row["video_id"]; !ok {
			row["video_id"] = vid.String()
		}
	}
	return json.Marshal(rows)
}

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(strings.TrimSpace(s)); err != nil {
		return u, err
	}
	return u, nil
}

func validateNestedRenderJob(ctx context.Context, tx pgx.Tx, owner, jobID pgtype.UUID, seen map[string]bool, depth int) error {
	if depth > 4 || seen[jobID.String()] {
		return fmt.Errorf("nested export source cycle or depth exceeded")
	}
	seen[jobID.String()] = true
	defer delete(seen, jobID.String())
	var creator string
	var snapshot, legacy []byte
	if err := tx.QueryRow(ctx, `SELECT created_by::text,document_snapshot,segments FROM stitch_jobs WHERE id=$1`, jobID).Scan(&creator, &snapshot, &legacy); err != nil {
		return fmt.Errorf("export %s is unavailable", jobID)
	}
	if creator != owner.String() && !sameWorkspaceCreator(ctx, owner, creator) {
		return fmt.Errorf("export %s is outside workspace", jobID)
	}
	var doc Document
	if len(snapshot) > 0 {
		var frozen RenderSnapshot
		if err := json.Unmarshal(snapshot, &frozen); err != nil {
			return fmt.Errorf("export %s has invalid snapshot", jobID)
		}
		doc = frozen.Document
	} else if len(legacy) > 0 {
		var rows []map[string]json.RawMessage
		if json.Unmarshal(legacy, &rows) != nil {
			return fmt.Errorf("export %s has invalid sources", jobID)
		}
		for _, row := range rows {
			for _, field := range []string{"video_id", "source_video_id"} {
				var raw string
				if json.Unmarshal(row[field], &raw) == nil && raw != "" {
					id, err := parseUUID(raw)
					if err != nil || !nestedVideoInWorkspace(ctx, tx, owner, id) {
						return fmt.Errorf("export %s source is unavailable", jobID)
					}
				}
			}
			if raw := row["legacy_metadata"]; len(raw) > 0 {
				var frozen struct {
					VideoID string `json:"video_id"`
				}
				if json.Unmarshal(raw, &frozen) == nil && frozen.VideoID != "" {
					id, err := parseUUID(frozen.VideoID)
					if err != nil || !nestedVideoInWorkspace(ctx, tx, owner, id) {
						return fmt.Errorf("export %s frozen source is unavailable", jobID)
					}
				}
			}
			if raw := row["clip_id"]; len(raw) > 0 {
				var idText string
				if json.Unmarshal(raw, &idText) == nil && idText != "" {
					id, err := parseUUID(idText)
					if err != nil || !nestedClipInWorkspace(ctx, tx, owner, id) {
						return fmt.Errorf("export %s clip source is unavailable", jobID)
					}
				}
			}
			for _, field := range []string{"export_job_id", "stitch_job_id"} {
				var idText string
				if json.Unmarshal(row[field], &idText) == nil && idText != "" {
					id, err := parseUUID(idText)
					if err != nil || validateNestedRenderJob(ctx, tx, owner, id, seen, depth+1) != nil {
						return fmt.Errorf("export %s nested source is unavailable", jobID)
					}
				}
			}
		}
		return nil
	}
	for _, segment := range doc.Segments {
		if segment.VideoID != "" {
			id, err := parseUUID(segment.VideoID)
			if err != nil || !nestedVideoInWorkspace(ctx, tx, owner, id) {
				return fmt.Errorf("export %s source is unavailable", jobID)
			}
		}
		if segment.ClipID != "" {
			id, err := parseUUID(segment.ClipID)
			if err != nil || !nestedClipInWorkspace(ctx, tx, owner, id) {
				return fmt.Errorf("export %s source is unavailable", jobID)
			}
		}
		if segment.ExportJobID != "" {
			id, err := parseUUID(segment.ExportJobID)
			if err != nil || validateNestedRenderJob(ctx, tx, owner, id, seen, depth+1) != nil {
				return fmt.Errorf("export %s source is unavailable", jobID)
			}
		}
	}
	return nil
}

func nestedVideoInWorkspace(ctx context.Context, tx pgx.Tx, owner, videoID pgtype.UUID) bool {
	tenant, enforce, err := workspaceTenant(ctx, owner)
	if err != nil {
		return false
	}
	var ok bool
	if enforce {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM videos WHERE id=$1 AND tenant_id=$2)`, videoID, tenant).Scan(&ok)
	} else {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM videos WHERE id=$1)`, videoID).Scan(&ok)
	}
	return err == nil && ok
}

func nestedClipInWorkspace(ctx context.Context, tx pgx.Tx, owner, clipID pgtype.UUID) bool {
	tenant, enforce, err := workspaceTenant(ctx, owner)
	if err != nil {
		return false
	}
	var ok bool
	if enforce {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM clips c JOIN videos v ON v.id=c.video_id WHERE c.id=$1 AND v.tenant_id=$2)`, clipID, tenant).Scan(&ok)
	} else {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM clips WHERE id=$1 AND created_by=$2)`, clipID, owner).Scan(&ok)
	}
	return err == nil && ok
}

// validateSources checks references and freezes mutable clip metadata in segments.
func validateSources(ctx context.Context, tx pgx.Tx, owner pgtype.UUID, d Document) (Document, error) {
	tenant, enforceTenant, err := workspaceTenant(ctx, owner)
	if err != nil {
		return d, err
	}
	for i := range d.Segments {
		s := &d.Segments[i]
		if s.ClipID != "" {
			cid, err := parseUUID(s.ClipID)
			if err != nil {
				return d, fmt.Errorf("invalid clip id: %w", err)
			}
			var legacyRaw map[string]json.RawMessage
			if len(s.Legacy) > 0 && json.Unmarshal(s.Legacy, &legacyRaw) != nil {
				return d, fmt.Errorf("clip %s has invalid legacy metadata", s.ClipID)
			}
			frozenExisting := len(legacyRaw["legacy_metadata"]) > 0
			var vid, videoTenant pgtype.UUID
			var start, end, duration float64
			var crops, shotList, filters []byte
			clipQuery := `SELECT c.video_id,c.start_ts,c.end_ts,c.duration,c.crops,c.shot_list,c.filter_stack,v.tenant_id FROM clips c JOIN videos v ON v.id=c.video_id WHERE c.id=$1`
			clipArgs := []any{cid}
			if !enforceTenant {
				clipQuery += ` AND c.created_by=$2`
				clipArgs = append(clipArgs, owner)
			}
			err = tx.QueryRow(ctx, clipQuery, clipArgs...).Scan(&vid, &start, &end, &duration, &crops, &shotList, &filters, &videoTenant)
			if err != nil {
				if err == pgx.ErrNoRows {
					return d, fmt.Errorf("clip %s is unavailable", s.ClipID)
				}
				return d, err
			}
			if enforceTenant && videoTenant.Bytes != tenant.Bytes {
				return d, fmt.Errorf("clip %s source is outside workspace", s.ClipID)
			}
			if !frozenExisting && s.DurationUS <= 0 {
				s.DurationUS = int64(duration * 1e6)
			}
			if !frozenExisting && s.SourceInUS == 0 {
				s.SourceInUS = int64(start * 1e6)
			}
			if !frozenExisting && s.VideoID != "" && s.VideoID != vid.String() {
				return d, fmt.Errorf("clip %s video reference mismatch", s.ClipID)
			}
			if !frozenExisting && (s.DurationUS <= 0 || float64(s.SourceInUS)/1e6 < start-0.000001 || float64(s.SourceInUS+s.DurationUS)/1e6 > end+0.000001) {
				return d, fmt.Errorf("clip %s exceeds source bounds", s.ClipID)
			}
			if !frozenExisting {
				s.VideoID = vid.String()
				if s.ClipStartUS == 0 {
					s.ClipStartUS = int64(start * 1e6)
				}
				if len(s.Crops) == 0 && nonEmptyJSONArray(crops) {
					_ = json.Unmarshal(crops, &s.Crops)
				}
				if len(s.Shots) == 0 && nonEmptyJSONArray(shotList) {
					_ = json.Unmarshal(shotList, &s.Shots)
				}
				metadata, _ := json.Marshal(frozenClip{ClipID: s.ClipID, VideoID: s.VideoID, StartUS: int64(start * 1e6), DurationUS: int64(duration * 1e6), EndUS: end, Crops: crops, ShotList: shotList, FilterStack: filters})
				raw := legacyRaw
				if raw == nil {
					raw = map[string]json.RawMessage{}
				}
				raw["legacy_metadata"] = metadata
				s.Legacy, _ = json.Marshal(raw)
			} else {
				var meta frozenClip
				if err := json.Unmarshal(legacyRaw["legacy_metadata"], &meta); err != nil || meta.VideoID == "" || meta.EndUS <= 0 || meta.DurationUS <= 0 {
					return d, fmt.Errorf("clip %s has invalid frozen source metadata", s.ClipID)
				}
				if s.VideoID != "" && s.VideoID != meta.VideoID {
					return d, fmt.Errorf("clip %s video reference mismatch", s.ClipID)
				}
				lo, hi := meta.StartUS, int64(meta.EndUS*1e6)
				if s.SourceInUS < lo || s.DurationUS <= 0 || s.SourceInUS+s.DurationUS > hi {
					return d, fmt.Errorf("clip %s exceeds frozen source bounds", s.ClipID)
				}
				if s.ClipStartUS == 0 {
					s.ClipStartUS = meta.StartUS
				}
				if len(s.Crops) == 0 && nonEmptyJSONArray(meta.Crops) {
					_ = json.Unmarshal(meta.Crops, &s.Crops)
					if len(s.Shots) == 0 && nonEmptyJSONArray(meta.ShotList) {
						_ = json.Unmarshal(meta.ShotList, &s.Shots)
					}
				}
			}
		}
		if s.VideoID != "" {
			vid, err := parseUUID(s.VideoID)
			if err != nil {
				return d, fmt.Errorf("invalid video id: %w", err)
			}
			var exists bool
			var videoDuration *int
			if err = tx.QueryRow(ctx, `SELECT duration_seconds FROM videos WHERE id=$1 AND ($2::boolean = false OR tenant_id=$3)`, vid, enforceTenant, tenant).Scan(&videoDuration); err != nil && err != pgx.ErrNoRows {
				return d, err
			}
			exists = videoDuration != nil || err == nil
			if !exists {
				return d, fmt.Errorf("video %s is unavailable", s.VideoID)
			}
			if videoDuration != nil && (s.SourceInUS < 0 || s.SourceInUS+s.DurationUS > int64(*videoDuration)*1_000_000) {
				return d, fmt.Errorf("video %s exceeds source bounds", s.VideoID)
			}
		}
		if s.ExportJobID != "" {
			jid, err := parseUUID(s.ExportJobID)
			if err != nil {
				return d, fmt.Errorf("invalid export id: %w", err)
			}
			var exists bool
			var creator string
			if err = tx.QueryRow(ctx, `SELECT created_by::text FROM stitch_jobs WHERE id=$1`, jid).Scan(&creator); err != nil && err != pgx.ErrNoRows {
				return d, err
			}
			if err == nil {
				exists = creator == owner.String() || sameWorkspaceCreator(ctx, owner, creator)
				if exists {
					if nestedErr := validateNestedRenderJob(ctx, tx, owner, jid, map[string]bool{}, 0); nestedErr != nil {
						return d, nestedErr
					}
				}
			}
			if !exists {
				return d, fmt.Errorf("export %s is unavailable", s.ExportJobID)
			}
		}
	}
	return d, Validate(d)
}

func nonEmptyJSONArray(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) == 0 {
		return false
	}
	return true
}
