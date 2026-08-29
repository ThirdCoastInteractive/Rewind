package captions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseYouTubeAuto(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "youtube-auto.vtt"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseString(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Dirty {
		t.Fatal("expected dirty YouTube auto-captions")
	}
	if len(doc.Cues) != 2 {
		t.Fatalf("cues = %#v (len %d), want 2 committed lines", doc.Cues, len(doc.Cues))
	}
	if doc.Cues[0].Text != "Shot fired. Shot fire." {
		t.Errorf("cue0 = %q", doc.Cues[0].Text)
	}
	if doc.Cues[1].Text != ">> Just had someone threatening the" {
		t.Errorf("cue1 = %q", doc.Cues[1].Text)
	}
	for _, c := range doc.Cues {
		if strings.Contains(c.Text, "<") || strings.Contains(c.Text, "&gt;") {
			t.Errorf("markup leaked: %q", c.Text)
		}
	}
	blob := PlainText(doc.Cues)
	if strings.Count(blob, "Shot fired") != 1 {
		t.Errorf("expected rolling window collapsed, got %q", blob)
	}
}

func TestParseWhisperClean(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "whisper-clean.vtt"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseString(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Cues) != 3 {
		t.Fatalf("cues = %#v", doc.Cues)
	}
	if doc.Cues[1].Text != "Windows are open" {
		t.Errorf("cue1 = %q", doc.Cues[1].Text)
	}
}

func TestParseStyleNoteDedup(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "style-note.vtt"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseString(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Cues) != 1 {
		t.Fatalf("expected consecutive duplicate collapsed, got %#v", doc.Cues)
	}
	if doc.Cues[0].Text != "Hello world" {
		t.Errorf("text = %q", doc.Cues[0].Text)
	}
	if doc.Cues[0].End < 3 {
		t.Errorf("dedup should extend end, got %v", doc.Cues[0].End)
	}
}

func TestParseEmpty(t *testing.T) {
	doc, err := ParseString("WEBVTT\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Cues) != 0 {
		t.Fatalf("got %#v", doc.Cues)
	}
}

func TestLangFromFilename(t *testing.T) {
	cases := map[string]string{
		"abc.captions.en.vtt":        "en",
		"abc.captions.en-US.vtt":     "en-us",
		"abc.captions.und.vtt":       "und",
		"abc.captions.video.vtt":     "und",
		"abc.captions.en.src.vtt":    "en",
		"something.en.vtt":           "en",
		"nope.vtt":                   "und",
	}
	for in, want := range cases {
		if got := LangFromFilename(in); got != want {
			t.Errorf("LangFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLooksDirty(t *testing.T) {
	if !LooksDirty([]byte("align:start position:0%\nhello")) {
		t.Fatal("settings should look dirty")
	}
	if LooksDirty([]byte("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello\n")) {
		t.Fatal("clean VTT should not look dirty")
	}
}

func TestWriteRoundTrip(t *testing.T) {
	doc, err := ParseString("WEBVTT\n\n00:00:01.000 --> 00:00:02.500\nHello there\n")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := WriteVTT(&b, doc.Cues); err != nil {
		t.Fatal(err)
	}
	again, err := ParseString(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Cues) != 1 || again.Cues[0].Text != "Hello there" {
		t.Fatalf("roundtrip = %#v", again.Cues)
	}
}
