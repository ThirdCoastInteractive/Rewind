// Package transcription stores partial ASR with explicit source-time coverage.
package transcription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"sort"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/captions"
	"thirdcoast.systems/rewind/pkg/utils/language"
)

// MergeRange replaces overlapping partial cues and shifts new cues into source time.
func MergeRange(old, relative []captions.Cue, start, end float64) []captions.Cue {
	out := make([]captions.Cue, 0, len(old)+len(relative))
	for _, c := range old {
		if c.Start < start || c.End > end {
			out = append(out, c)
		}
	}
	for _, c := range relative {
		c.Start = max(start, c.Start+start)
		c.End = min(end, c.End+start)
		if c.End > c.Start {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// StoreRange runs inside the caller's transaction and never replaces a complete transcript.
func StoreRange(ctx context.Context, q *db.Queries, id pgtype.UUID, lang string, relative []captions.Cue, start, end float64) error {
	if len(MergeRange(nil, relative, start, end)) == 0 {
		return fmt.Errorf("no speech recognized in requested range")
	}
	var tag language.Tag
	if err := tag.Scan(lang); err != nil {
		return err
	}
	if err := q.LockTranscriptForContext(ctx, id); err != nil {
		return err
	}
	old, err := q.GetVideoTranscriptByLanguage(ctx, &db.GetVideoTranscriptByLanguageParams{VideoID: id, Lang: tag})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		old = nil
	}
	var cues []captions.Cue
	ranges := [][2]float64{}
	if old != nil {
		if len(old.Coverage) == 0 {
			return nil
		}
		if err := json.Unmarshal(old.Cues, &cues); err != nil {
			return err
		}
		if err := json.Unmarshal(old.Coverage, &ranges); err != nil {
			return err
		}
	}
	cues = MergeRange(cues, relative, start, end)
	ranges = append(ranges, [2]float64{start, end})
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
	merged := [][2]float64{}
	for _, r := range ranges {
		if len(merged) > 0 && r[0] <= merged[len(merged)-1][1] {
			merged[len(merged)-1][1] = max(merged[len(merged)-1][1], r[1])
		} else {
			merged = append(merged, r)
		}
	}
	raw, _ := json.Marshal(cues)
	coverage, _ := json.Marshal(merged)
	return q.UpsertPartialTranscript(ctx, &db.UpsertPartialTranscriptParams{VideoID: id, Lang: tag, Text: captions.PlainText(cues), Cues: raw, Coverage: coverage})
}
