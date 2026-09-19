package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	xtlang "golang.org/x/text/language"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/captions"
	rewindlang "thirdcoast.systems/rewind/pkg/utils/language"
)

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

func loadTranscriptCues(ctx context.Context, q *db.Queries, videoID pgtype.UUID) ([]captions.Cue, string, error) {
	tr, err := q.GetVideoTranscript(ctx, videoID)
	if err != nil || tr == nil {
		return nil, "", fmt.Errorf("no transcript")
	}
	cues, err := captions.CuesFromStoredTranscript(tr.Cues, tr.Raw)
	if err != nil {
		return nil, "", err
	}
	return cues, tr.Text, nil
}
