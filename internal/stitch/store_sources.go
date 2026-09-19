package stitch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

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
		var crops, shotList, filters []byte
		if err = tx.QueryRow(ctx, `SELECT video_id,start_ts,end_ts,duration,crops,shot_list,filter_stack FROM clips WHERE id=$1 AND created_by=$2`, id, owner).Scan(&vid, &start, &end, &dur, &crops, &shotList, &filters); err != nil {
			return nil, err
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

// validateSources checks references and freezes mutable clip metadata in segments.
func validateSources(ctx context.Context, tx pgx.Tx, owner pgtype.UUID, d Document) (Document, error) {
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
			var vid pgtype.UUID
			var start, end, duration float64
			var crops, shotList, filters []byte
			err = tx.QueryRow(ctx, `SELECT video_id,start_ts,end_ts,duration,crops,shot_list,filter_stack FROM clips WHERE id=$1 AND created_by=$2`, cid, owner).Scan(&vid, &start, &end, &duration, &crops, &shotList, &filters)
			if err != nil {
				if err == pgx.ErrNoRows {
					return d, fmt.Errorf("clip %s is unavailable", s.ClipID)
				}
				return d, err
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
			if err = tx.QueryRow(ctx, `SELECT duration_seconds FROM videos WHERE id=$1`, vid).Scan(&videoDuration); err != nil && err != pgx.ErrNoRows {
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
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stitch_jobs WHERE id=$1 AND created_by=$2)`, jid, owner).Scan(&exists); err != nil {
				return d, err
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
