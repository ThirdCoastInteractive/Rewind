package ffmpeg

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEscapeDrawtextApostrophe(t *testing.T) {
	t.Parallel()
	got := escapeDrawtext("CALLEN'S TESLA")
	if got != `CALLEN\'S TESLA` {
		t.Fatalf("got %q", got)
	}
}

func TestStitchTitleCardUsesTomorrowAndKeepsPunctuation(t *testing.T) {
	t.Parallel()
	bold, regular, err := TitleFontPaths()
	if err != nil {
		t.Fatal(err)
	}
	if bold == "" || regular == "" {
		t.Fatal("expected extracted Tomorrow font paths")
	}

	cmd := StitchCommand(
		[]Segment{{
			Type:          SegmentTitle,
			TitleDuration: 3 * time.Second,
			BgColor:       "#000000",
			Text:          "CALLEN'S TESLA",
			Subtitle:      "Ben Avery & Devan Costa · Sept 2026",
			TextColor:     "#ffffff",
			FontSize:      72,
			Position:      "center",
		}},
		[]*Transition{nil},
		"out.mp4",
		nil, nil,
		1920, 1080,
	)
	joined := strings.Join(cmd.Build(), " ")
	if !strings.Contains(joined, "UnifrakturCook-Bold.ttf") && !strings.Contains(joined, "Tomorrow-Bold.ttf") {
		t.Fatalf("missing title fontfile in %s", joined)
	}
	if !strings.Contains(joined, "Tomorrow-Regular.ttf") {
		t.Fatalf("missing regular fontfile in %s", joined)
	}
	if !strings.Contains(joined, "textfile=") {
		t.Fatalf("expected textfile= to avoid lavfi quoting: %s", joined)
	}
	if !strings.Contains(joined, "@0.55") && !strings.Contains(joined, "@0.5") {
		t.Fatalf("subtitle should use faded fontcolor: %s", joined)
	}
	if strings.Contains(joined, "c=#") || strings.Contains(joined, "fontcolor=#") {
		t.Fatalf("# in lavfi is a comment: %s", joined)
	}
}

func TestRenderTitleCardSmoke(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	bold, regular, err := TitleFontPaths()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "title-go.jpg")
	lavfi := "color=c=0x000000:s=1920x1080:d=0.2:r=30," +
		titleDrawtext(bold, "CALLEN'S TESLA", 72, "0xFFFFFF", "(w-text_w)/2", "(h-text_h)/2-40") + "," +
		titleDrawtext(regular, "Ben Avery & Devan Costa · Sept 2026", 36, "0xFFFFFF@0.60", "(w-text_w)/2", "(h-text_h)/2+40")
	cmd := exec.Command(ffmpegPath, "-hide_banner", "-y", "-f", "lavfi", "-i", lavfi, "-frames:v", "1", out)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v\n%s\nlavfi=%s", err, b, lavfi)
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() < 8_000 {
		t.Fatalf("expected a rendered frame, err=%v size=%v\n%s", err, st, b)
	}
}
