package archive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	xtlang "golang.org/x/text/language"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/captions"
)

type fakeTranscripts struct {
	arg *db.UpsertVideoTranscriptParams
}

func (f *fakeTranscripts) UpsertVideoTranscript(_ context.Context, arg *db.UpsertVideoTranscriptParams) error {
	f.arg = arg
	return nil
}

func TestImportTranscriptCleansAndDefaultsLang(t *testing.T) {
	store := &fakeTranscripts{}
	vtt := "WEBVTT\n\n00:00:00.000 --> 00:00:01.500\nHello &amp; welcome\n"
	videoID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	if err := importTranscriptWith(context.Background(), store, videoID, "", vtt); err != nil {
		t.Fatal(err)
	}
	if store.arg == nil {
		t.Fatal("missing upsert")
	}
	if store.arg.VideoID.String() != videoID {
		t.Fatalf("video id %s", store.arg.VideoID.String())
	}
	if xtlang.Tag(store.arg.Lang).String() != "en" {
		t.Fatalf("lang %s", xtlang.Tag(store.arg.Lang).String())
	}
	if store.arg.Format != "vtt" || store.arg.Text != "Hello & welcome" || store.arg.Raw != vtt {
		t.Fatalf("stored %+v", store.arg)
	}
	var cues []captions.Cue
	if err := json.Unmarshal(store.arg.Cues, &cues); err != nil {
		t.Fatal(err)
	}
	if len(cues) != 1 || cues[0].Text != "Hello & welcome" || cues[0].Start != 0 || cues[0].End != 1.5 {
		t.Fatalf("cues %+v", cues)
	}
}

func TestImportTranscriptKeepsLangAndRejectsEmpty(t *testing.T) {
	store := &fakeTranscripts{}
	vtt := "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHola\n"
	videoID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	if err := importTranscriptWith(context.Background(), store, videoID, "es", vtt); err != nil {
		t.Fatal(err)
	}
	if xtlang.Tag(store.arg.Lang).String() != "es" {
		t.Fatalf("lang %s", xtlang.Tag(store.arg.Lang).String())
	}
	empty := &fakeTranscripts{}
	err := importTranscriptWith(context.Background(), empty, videoID, "en", "WEBVTT\n\n")
	if err == nil || !strings.Contains(err.Error(), "empty transcript after parse") {
		t.Fatalf("err %v", err)
	}
	if empty.arg != nil {
		t.Fatal("empty transcript was stored")
	}
	if err := importTranscriptWith(context.Background(), empty, "nope", "en", vtt); err == nil {
		t.Fatal("invalid video id")
	}
}
