package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	xtlang "golang.org/x/text/language"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/captions"
	rewindlang "thirdcoast.systems/rewind/pkg/utils/language"
)

func findCaptionFilePath(infoPath string, spoolDir string) (string, string, bool) {
	// Returns (path, lang, ok). Manual English VTT wins over auto-caption filenames.
	seen := map[string]bool{}
	var candidates []string
	addGlob := func(pattern string) {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return
		}
		for _, p := range matches {
			if seen[p] {
				continue
			}
			seen[p] = true
			candidates = append(candidates, p)
		}
	}

	if strings.TrimSpace(infoPath) != "" && strings.HasSuffix(infoPath, ".info.json") {
		base := strings.TrimSuffix(infoPath, ".info.json")
		addGlob(base + "*.vtt")
		addGlob(filepath.Join(filepath.Dir(infoPath), "*.vtt"))
	}
	if strings.TrimSpace(spoolDir) != "" {
		addGlob(filepath.Join(spoolDir, "*.vtt"))
	}

	picked := pickPreferredCaptionFile(candidates)
	if picked == "" {
		return "", "", false
	}
	return picked, captions.LangFromFilename(picked), true
}

const skipCaptionRank = 100

// captionPreferenceRank scores a subtitle filename. Lower is better:
//
//	0 exact manual English (*.en.vtt)
//	1 other manual English (en-US, …)
//	2 auto English (*en.auto*, *-orig-*, automatic)
//	3 other manual languages
//	4 remaining auto/unknown
func captionPreferenceRank(name string) int {
	base := strings.ToLower(filepath.Base(name))
	if !strings.HasSuffix(base, ".vtt") || strings.HasSuffix(base, ".src.vtt") {
		return skipCaptionRank
	}
	auto := isAutoCaptionFilename(base)
	en := isEnglishCaptionFilename(base)
	switch {
	case en && !auto && strings.HasSuffix(base, ".en.vtt"):
		return 0
	case en && !auto:
		return 1
	case en && auto:
		return 2
	case !auto:
		return 3
	default:
		return 4
	}
}

func isAutoCaptionFilename(name string) bool {
	n := strings.ToLower(filepath.Base(name))
	switch {
	case strings.Contains(n, "automatic"):
		return true
	case strings.Contains(n, "en.auto"), strings.Contains(n, "en-auto"):
		return true
	case strings.Contains(n, "-orig-"), strings.Contains(n, "-orig."), strings.Contains(n, ".orig."):
		return true
	default:
		return false
	}
}

func isEnglishCaptionFilename(name string) bool {
	n := strings.ToLower(filepath.Base(name))
	lang := captions.LangFromFilename(n)
	if lang == "en" || strings.HasPrefix(lang, "en-") {
		return true
	}
	stem := strings.TrimSuffix(n, ".vtt")
	stem = strings.TrimSuffix(stem, ".src")
	switch {
	case strings.HasSuffix(stem, ".en"), strings.Contains(stem, ".en."), strings.Contains(stem, ".en-"):
		return true
	case strings.Contains(stem, "en.auto"), strings.Contains(stem, "en-auto"), strings.Contains(stem, "en-orig"):
		return true
	default:
		return false
	}
}

func pickPreferredCaptionFile(paths []string) string {
	type cand struct {
		path string
		rank int
	}
	var cs []cand
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		rank := captionPreferenceRank(p)
		if rank >= skipCaptionRank {
			continue
		}
		cs = append(cs, cand{path: p, rank: rank})
	}
	if len(cs) == 0 {
		return ""
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].rank != cs[j].rank {
			return cs[i].rank < cs[j].rank
		}
		return cs[i].path < cs[j].path
	})
	return cs[0].path
}

func ingestTranscriptFile(ctx context.Context, q *db.Queries, videoID pgtype.UUID, lang string, path string) error {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("open captions: %w", err)
	}

	doc, err := captions.CleanFile(path)
	if err != nil {
		return fmt.Errorf("clean vtt: %w", err)
	}
	text := captions.PlainText(doc.Cues)
	if strings.TrimSpace(text) == "" {
		return errors.New("empty transcript after parse")
	}

	if lang == "" || lang == "video" || lang == "captions" {
		lang = captions.LangFromFilename(path)
	}
	parsedLang, err := xtlang.Parse(lang)
	if err != nil {
		parsedLang = xtlang.Und
	}

	cuesJSON, err := json.Marshal(doc.Cues)
	if err != nil {
		return fmt.Errorf("marshal cues: %w", err)
	}

	return q.UpsertVideoTranscript(ctx, &db.UpsertVideoTranscriptParams{
		VideoID: videoID,
		Lang:    rewindlang.Tag(parsedLang),
		Format:  "vtt",
		Text:    text,
		Raw:     string(rawBytes),
		Cues:    cuesJSON,
	})
}

// materializeStoredTranscript restores the canonical player sidecar without
// running speech recognition again. This covers transcript producers that
// persist timed cues directly in Postgres but do not emit a .vtt file.
func materializeStoredTranscript(ctx context.Context, q *db.Queries, videoID pgtype.UUID, outputDir, videoIDText string) (string, string, bool) {
	transcript, err := q.GetVideoTranscript(ctx, videoID)
	if err != nil || transcript == nil {
		return "", "", false
	}
	cues, err := captions.CuesFromStoredTranscript(transcript.Cues, transcript.Raw)
	if err != nil {
		return "", "", false
	}
	lang := xtlang.Tag(transcript.Lang).String()
	if lang == "" || lang == "und" {
		lang = "und"
	}
	dest := filepath.Join(outputDir, videoIDText+".captions."+lang+".vtt")
	if err := captions.WriteVTTFile(dest, cues); err != nil {
		return "", "", false
	}
	return dest, lang, true
}

func subtitleKindFromInfo(raw []byte) string {
	return subtitleKind(raw, "")
}

// subtitleKindFromFilename classifies a yt-dlp sidecar when info.json is missing
// or ambiguous. Manual English (*.en.vtt, en-US) beats auto names (*en.auto*,
// *-orig-*, "automatic").
func subtitleKindFromFilename(name string) string {
	if strings.TrimSpace(name) == "" {
		return "automatic"
	}
	if isAutoCaptionFilename(name) {
		return "automatic"
	}
	if isEnglishCaptionFilename(name) {
		return "manual"
	}
	return "automatic"
}

// subtitleKind prefers publisher-provided English tracks in info.json. When the
// JSON has no en/en-* entry under subtitles, the caption filename is used.
func subtitleKind(raw []byte, captionPath string) string {
	var info struct {
		Subtitles         map[string]json.RawMessage `json:"subtitles"`
		AutomaticCaptions map[string]json.RawMessage `json:"automatic_captions"`
	}
	if json.Unmarshal(raw, &info) == nil {
		for lang := range info.Subtitles {
			if lang == "en" || strings.HasPrefix(lang, "en-") {
				return "manual"
			}
		}
	}
	if captionPath != "" {
		return subtitleKindFromFilename(captionPath)
	}
	return "automatic"
}
