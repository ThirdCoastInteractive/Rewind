package stitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	xlanguage "golang.org/x/text/language"

	"thirdcoast.systems/rewind/pkg/captions"
	stitchlang "thirdcoast.systems/rewind/pkg/utils/language"
)

type sourceImportCue struct {
	Start float64            `json:"start"`
	End   float64            `json:"end"`
	Text  string             `json:"text"`
	Words []sourceImportWord `json:"words"`
}
type sourceImportWord struct {
	Text  string   `json:"text"`
	Word  string   `json:"word"`
	Start *float64 `json:"start"`
	End   *float64 `json:"end"`
}

func parseImportCues(raw json.RawMessage, fallback string) ([]sourceImportCue, error) {
	var out []sourceImportCue
	if len(raw) > 0 && json.Unmarshal(raw, &out) == nil && len(out) > 0 {
		return out, nil
	}
	c, e := captions.CuesFromStoredTranscript(raw, fallback)
	if e != nil {
		return nil, e
	}
	for _, x := range c {
		out = append(out, sourceImportCue{Start: x.Start, End: x.End, Text: x.Text})
	}
	return out, nil
}

// selectVerifiedImportWords returns only complete source words inside the
// clipped cue bounds. A cue is considered aligned only when the source word
// sequence exactly accounts for its text; otherwise callers must keep the cue
// unaligned rather than manufacture timing for text that is not evidenced.
func selectVerifiedImportWords(c sourceImportCue, clipStartUS, clipEndUS int64) ([]sourceImportWord, bool) {
	if len(c.Words) == 0 || clipEndUS <= clipStartUS || !sourceWordsMatchText(c.Words, c.Text) {
		return nil, false
	}
	cueStartUS := int64(c.Start * 1e6)
	cueEndUS := int64(c.End * 1e6)
	if cueEndUS <= cueStartUS {
		return nil, false
	}
	selected := make([]sourceImportWord, 0, len(c.Words))
	var last int64 = -1
	for _, w := range c.Words {
		if w.Start == nil || w.End == nil {
			return nil, false
		}
		ws, we := int64(*w.Start*1e6), int64(*w.End*1e6)
		wordText := strings.TrimSpace(w.Text)
		if wordText == "" {
			wordText = strings.TrimSpace(w.Word)
		}
		if ws < cueStartUS || we > cueEndUS || we <= ws || ws < last || wordText == "" {
			return nil, false
		}
		last = we
		// Do not clip a word's timestamps. Keeping only words fully contained in
		// the visible cue interval preserves verified timing exactly.
		if ws >= clipStartUS && we <= clipEndUS {
			w.Text = wordText
			selected = append(selected, w)
		}
	}
	if len(selected) == 0 {
		return nil, false
	}
	return selected, true
}

func sourceWordsMatchText(words []sourceImportWord, text string) bool {
	parts := make([]string, 0, len(words))
	for _, w := range words {
		wordText := strings.TrimSpace(w.Text)
		if wordText == "" {
			wordText = strings.TrimSpace(w.Word)
		}
		if wordText == "" {
			return false
		}
		parts = append(parts, wordText)
	}
	return normalizeTeaserText(strings.Join(parts, " ")) == normalizeTeaserText(text)
}

func prepareImportOperations(ctx context.Context, tx pgx.Tx, d Document, ops []Operation) ([]Operation, error) {
	out := make([]Operation, 0, len(ops))
	for _, op := range ops {
		if op.Type == "style_teaser_captions" {
			if op.TeaserCaption == nil {
				return nil, errors.New("teaser caption options are required")
			}
			options := *op.TeaserCaption
			if err := ValidateTeaserCaptionOptions(options); err != nil {
				return nil, err
			}
			working := d
			if strings.TrimSpace(op.ImportSegmentID) != "" {
				lang := strings.TrimSpace(op.ImportLanguage)
				if lang == "" {
					lang = "en"
				}
				before := map[string]bool{}
				for _, c := range d.Captions {
					before[c.ID] = true
				}
				imported, err := prepareImportOperations(ctx, tx, d, []Operation{{Type: "import_source_captions", TargetID: op.ImportSegmentID, Language: lang}})
				if err != nil {
					return nil, err
				}
				if len(imported) != 1 {
					return nil, errors.New("teaser caption import expansion failed")
				}
				var applyErr error
				working, _, applyErr = Apply(d, imported)
				if applyErr != nil {
					return nil, applyErr
				}
				out = append(out, imported...)
				if len(options.CaptionIDs) == 0 {
					for _, c := range working.Captions {
						if !before[c.ID] && c.SegmentID == op.ImportSegmentID && c.Language == lang {
							options.CaptionIDs = append(options.CaptionIDs, c.ID)
						}
					}
					if len(options.CaptionIDs) == 0 {
						for _, c := range working.Captions {
							if c.SegmentID == op.ImportSegmentID && c.Language == lang {
								options.CaptionIDs = append(options.CaptionIDs, c.ID)
							}
						}
					}
				}
			}
			formatted, err := StyleTeaserCaptions(working, options)
			if err != nil {
				return nil, err
			}
			styleOps := TeaserCaptionOperationsForDocument(working, formatted)
			if len(out)+len(styleOps) > 100 {
				return nil, fmt.Errorf("teaser caption operation expansion exceeds 100 operations")
			}
			out = append(out, styleOps...)
			continue
		}
		if op.Type != "import_source_captions" {
			out = append(out, op)
			continue
		}
		if len(op.Captions) > 0 {
			return nil, errors.New("import_source_captions does not accept captions")
		}
		var seg *Segment
		for i := range d.Segments {
			if d.Segments[i].ID == op.TargetID {
				seg = &d.Segments[i]
			}
		}
		if seg == nil {
			return nil, errors.New("segment not found")
		}
		vid, e := parseUUID(seg.VideoID)
		if e != nil {
			return nil, e
		}
		var cuesJSON, raw []byte
		if e = tx.QueryRow(ctx, `SELECT cues,raw FROM video_transcripts WHERE video_id=$1 AND lang=$2`, vid, stitchlang.Tag(xlanguage.Make(op.Language))).Scan(&cuesJSON, &raw); e != nil {
			return nil, e
		}
		cues, e := parseImportCues(cuesJSON, string(raw))
		if e != nil {
			return nil, e
		}
		caps := make([]Caption, 0, len(cues))
		for _, c := range cues {
			start := int64(c.Start * 1e6)
			end := int64(c.End * 1e6)
			if end <= seg.SourceInUS || start >= seg.SourceInUS+seg.DurationUS {
				continue
			}
			if start < seg.SourceInUS {
				start = seg.SourceInUS
			}
			if end > seg.SourceInUS+seg.DurationUS {
				end = seg.SourceInUS + seg.DurationUS
			}
			dup := false
			for _, old := range d.Captions {
				if old.SegmentID == seg.ID && old.Language == op.Language && old.SourceVideoID == seg.VideoID && old.SourceStartUS == start && old.SourceEndUS == end {
					dup = true
				}
			}
			if !dup {
				cap := Caption{ID: newID(), SegmentID: seg.ID, Text: c.Text, Language: op.Language, StartUS: seg.StartUS + start - seg.SourceInUS, EndUS: seg.StartUS + end - seg.SourceInUS, SourceVideoID: seg.VideoID, SourceStartUS: start, SourceEndUS: end, Alignment: "unaligned"}
				selected, valid := selectVerifiedImportWords(c, start, end)
				if valid {
					parts := make([]string, 0, len(selected))
					for _, w := range selected {
						wordText := strings.TrimSpace(w.Text)
						if wordText == "" {
							wordText = strings.TrimSpace(w.Word)
						}
						ws, we := int64(*w.Start*1e6), int64(*w.End*1e6)
						parts = append(parts, wordText)
						cap.Words = append(cap.Words, Word{Text: wordText, StartUS: seg.StartUS + ws - seg.SourceInUS, EndUS: seg.StartUS + we - seg.SourceInUS})
					}
					// A clipped cue contains a contiguous, evidenced subset of the
					// source words. Keep the original cue spelling when complete;
					// otherwise make the stored text match exactly those words.
					if len(selected) != len(c.Words) {
						cap.Text = strings.Join(parts, " ")
					}
					cap.Alignment = "valid"
				} else {
					cap.Words = nil
				}
				caps = append(caps, cap)
			}
		}
		out = append(out, Operation{Type: "import_captions", TargetID: seg.ID, Language: op.Language, Captions: caps})
	}
	return out, nil
}

// ImportCaptions commits a typed source-caption import request through the normal idempotent path.
func (s *Store) ImportCaptions(ctx context.Context, owner, project pgtype.UUID, expected int64, key string, actor Actor, segmentID, lang string) (Result, error) {
	return s.Commit(ctx, owner, project, expected, key, actor, "Import source captions", []Operation{{Type: "import_source_captions", TargetID: segmentID, Language: lang}})
}
