package ingest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPickPreferredCaptionFile(t *testing.T) {
	t.Parallel()

	got := pickPreferredCaptionFile([]string{
		"youtube_abc.en-orig.vtt",
		"youtube_abc.en.auto.vtt",
		"youtube_abc.en.vtt",
		"youtube_abc.en-US.vtt",
	})
	if got != "youtube_abc.en.vtt" {
		t.Fatalf("prefer manual English, got %q", got)
	}

	got = pickPreferredCaptionFile([]string{
		"clip.en.auto.vtt",
		"clip.en-orig.vtt",
		"clip.automatic.en.vtt",
	})
	if got != "clip.en-orig.vtt" && !isAutoCaptionFilename(got) {
		t.Fatalf("auto-only set should still pick an auto English file, got %q", got)
	}
	if captionPreferenceRank(got) != 2 {
		t.Fatalf("auto-only pick %q rank = %d, want 2", got, captionPreferenceRank(got))
	}

	if pickPreferredCaptionFile([]string{"note.src.vtt", "readme.txt"}) != "" {
		t.Fatal("src/non-vtt files should be skipped")
	}
	if pickPreferredCaptionFile(nil) != "" {
		t.Fatal("empty set should return empty")
	}
}

func TestCaptionPreferenceRank(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		rank int
		auto bool
	}{
		{"video.en.vtt", 0, false},
		{"video.en-US.vtt", 1, false},
		{"video.en.auto.vtt", 2, true},
		{"video.en-auto.vtt", 2, true},
		{"video.en-orig.vtt", 2, true},
		{"video-orig-lang.en.vtt", 2, true},
		{"video.automatic.vtt", 4, true},
		{"video.de.vtt", 3, false},
		{"video.src.vtt", skipCaptionRank, false},
	}
	for _, tc := range cases {
		if got := captionPreferenceRank(tc.name); got != tc.rank {
			t.Errorf("captionPreferenceRank(%q) = %d, want %d", tc.name, got, tc.rank)
		}
		if got := isAutoCaptionFilename(tc.name); got != tc.auto {
			t.Errorf("isAutoCaptionFilename(%q) = %v, want %v", tc.name, got, tc.auto)
		}
	}
}

func TestFindCaptionFilePathPrefersManualEnglish(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWrite := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("WEBVTT\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("youtube_abc.en.auto.vtt")
	mustWrite("youtube_abc.en-orig.vtt")
	mustWrite("youtube_abc.en.vtt")
	info := filepath.Join(dir, "youtube_abc.info.json")
	if err := os.WriteFile(info, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	path, lang, ok := findCaptionFilePath(info, dir)
	if !ok {
		t.Fatal("expected a caption file")
	}
	if filepath.Base(path) != "youtube_abc.en.vtt" {
		t.Fatalf("path = %q, want youtube_abc.en.vtt", path)
	}
	if lang != "en" {
		t.Fatalf("lang = %q, want en", lang)
	}

	autoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(autoDir, "only.en-orig.vtt"), []byte("WEBVTT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, _, ok = findCaptionFilePath("", autoDir)
	if !ok || filepath.Base(path) != "only.en-orig.vtt" {
		t.Fatalf("auto fallback = %q ok=%v", path, ok)
	}

	empty := t.TempDir()
	if _, _, ok := findCaptionFilePath("", empty); ok {
		t.Fatal("empty spool should not find captions")
	}
}

func TestSubtitleKindFromInfo(t *testing.T) {
	t.Parallel()

	if got := subtitleKindFromInfo([]byte(`{"subtitles":{"en":[{}]}}`)); got != "manual" {
		t.Fatalf("en subtitles = %q, want manual", got)
	}
	if got := subtitleKindFromInfo([]byte(`{"subtitles":{"en-US":[{}]}}`)); got != "manual" {
		t.Fatalf("en-US subtitles = %q, want manual", got)
	}
	if got := subtitleKindFromInfo([]byte(`{"subtitles":{"de":[{}]},"automatic_captions":{"en":[{}]}}`)); got != "automatic" {
		t.Fatalf("auto-only English = %q, want automatic", got)
	}
	if got := subtitleKindFromInfo([]byte(`not json`)); got != "automatic" {
		t.Fatalf("invalid json = %q, want automatic", got)
	}
}

func TestSubtitleKindFromFilename(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"youtube_abc.en.vtt":      "manual",
		"youtube_abc.en-US.vtt":   "manual",
		"youtube_abc.en.auto.vtt": "automatic",
		"youtube_abc.en-orig.vtt": "automatic",
		"clip.automatic.en.vtt":   "automatic",
		"":                        "automatic",
	}
	for in, want := range cases {
		if got := subtitleKindFromFilename(in); got != want {
			t.Errorf("subtitleKindFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSubtitleKindFilenameFallback(t *testing.T) {
	t.Parallel()

	ambiguous := []byte(`{"subtitles":{},"automatic_captions":{"en":[{}]}}`)
	if got := subtitleKind(ambiguous, "video.en.vtt"); got != "manual" {
		t.Fatalf("ambiguous info + manual filename = %q, want manual", got)
	}
	if got := subtitleKind(ambiguous, "video.en-orig.vtt"); got != "automatic" {
		t.Fatalf("ambiguous info + auto filename = %q, want automatic", got)
	}
	if got := subtitleKind([]byte(`{"subtitles":{"en":[{}]}}`), "video.en-orig.vtt"); got != "manual" {
		t.Fatalf("info.json manual should win over auto filename, got %q", got)
	}
}
