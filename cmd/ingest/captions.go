package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	xtlang "golang.org/x/text/language"
	"thirdcoast.systems/rewind/internal/db"
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
				return c.path, c.lang, true
			}
		}
	}

	if strings.TrimSpace(spoolDir) == "" {
		return "", "", false
	}

	// Prefer English if present.
	matches, err := filepath.Glob(filepath.Join(spoolDir, "*.en.vtt"))
	if err == nil && len(matches) > 0 {
		if _, err := os.Stat(matches[0]); err == nil {
			return matches[0], "en", true
		}
	}
	matches, err = filepath.Glob(filepath.Join(spoolDir, "*.vtt"))
	if err == nil && len(matches) > 0 {
		if _, err := os.Stat(matches[0]); err == nil {
			return matches[0], "und", true
		}
	}
	return "", "", false
}

func parseVTTToText(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 1024*64)
	scanner.Buffer(buf, 16*1024*1024)

	var out strings.Builder
	lastBlank := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			lastBlank = true
			continue
		}
		// Skip WEBVTT header and cue identifiers.
		if strings.EqualFold(line, "WEBVTT") {
			continue
		}
		// Skip timestamps.
		if strings.Contains(line, "-->") {
			continue
		}
		// Skip numeric cue ids.
		isNumeric := true
		for i := 0; i < len(line); i++ {
			c := line[i]
			if c < '0' || c > '9' {
				isNumeric = false
				break
			}
		}
		if isNumeric {
			continue
		}

		if !lastBlank {
			out.WriteByte(' ')
		}
		out.WriteString(line)
		lastBlank = false
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func ingestTranscriptFile(ctx context.Context, q *db.Queries, videoID pgtype.UUID, lang string, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open captions: %w", err)
	}
	defer f.Close()

	rawBytes, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("read captions: %w", err)
	}

	text, err := parseVTTToText(strings.NewReader(string(rawBytes)))
	if err != nil {
		return fmt.Errorf("parse vtt: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("empty transcript after parse")
	}

	parsedLang, err := xtlang.Parse(lang)
	if err != nil {
		parsedLang = xtlang.Und
	}

	return q.UpsertVideoTranscript(ctx, &db.UpsertVideoTranscriptParams{
		VideoID: videoID,
		Lang:    rewindlang.Tag(parsedLang),
		Format:  "vtt",
		Text:    text,
		Raw:     string(rawBytes),
	})
}
