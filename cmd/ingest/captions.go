package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	xtlang "golang.org/x/text/language"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/captions"
	rewindlang "thirdcoast.systems/rewind/pkg/utils/language"
)

func findCaptionFilePath(infoPath string, spoolDir string) (string, string, bool) {
	// Returns (path, lang, ok)
	if strings.TrimSpace(infoPath) != "" && strings.HasSuffix(infoPath, ".info.json") {
		base := strings.TrimSuffix(infoPath, ".info.json")
		candidates := []struct {
			path string
			lang string
		}{
			{path: base + ".en.vtt", lang: "en"},
			{path: base + ".vtt", lang: "und"},
		}
		for _, c := range candidates {
			if _, err := os.Stat(c.path); err == nil {
				return c.path, captions.LangFromFilename(c.path), true
			}
		}
	}

	if strings.TrimSpace(spoolDir) == "" {
		return "", "", false
	}

	matches, err := filepath.Glob(filepath.Join(spoolDir, "*.en.vtt"))
	if err == nil && len(matches) > 0 {
		if _, err := os.Stat(matches[0]); err == nil {
			return matches[0], "en", true
		}
	}
	matches, err = filepath.Glob(filepath.Join(spoolDir, "*.vtt"))
	if err == nil && len(matches) > 0 {
		if _, err := os.Stat(matches[0]); err == nil {
			return matches[0], captions.LangFromFilename(matches[0]), true
		}
	}
	return "", "", false
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
