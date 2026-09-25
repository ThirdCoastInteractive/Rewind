package archive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	xtlang "golang.org/x/text/language"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/captions"
	rewindlang "thirdcoast.systems/rewind/pkg/utils/language"
)

type transcriptStore interface {
	UpsertVideoTranscript(context.Context, *db.UpsertVideoTranscriptParams) error
}

// ImportTranscript cleans vtt the same way caption ingest does and upserts
// video_transcripts. lang defaults to en. An empty transcript after parse is an error.
func ImportTranscript(ctx context.Context, videoID, lang, vtt string) error {
	dbc, err := boundDB()
	if err != nil {
		return err
	}
	return importTranscriptWith(ctx, dbc.Queries(ctx), videoID, lang, vtt)
}

func importTranscriptWith(ctx context.Context, q transcriptStore, videoID, lang, vtt string) error {
	id, err := parsePGUUID(videoID)
	if err != nil {
		return err
	}
	doc, err := captions.ParseString(vtt)
	if err != nil {
		return fmt.Errorf("archive: clean vtt: %w", err)
	}
	text := captions.PlainText(doc.Cues)
	if strings.TrimSpace(text) == "" {
		return errors.New("empty transcript after parse")
	}
	if strings.TrimSpace(lang) == "" {
		lang = "en"
	}
	parsed, err := xtlang.Parse(lang)
	if err != nil || parsed == xtlang.Und {
		return fmt.Errorf("archive: transcript lang %q", lang)
	}
	cuesJSON, err := json.Marshal(doc.Cues)
	if err != nil {
		return fmt.Errorf("archive: marshal cues: %w", err)
	}
	return q.UpsertVideoTranscript(ctx, &db.UpsertVideoTranscriptParams{
		VideoID: id,
		Lang:    rewindlang.Tag(parsed),
		Format:  "vtt",
		Text:    text,
		Raw:     vtt,
		Cues:    cuesJSON,
	})
}
