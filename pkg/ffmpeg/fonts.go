package ffmpeg

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"thirdcoast.systems/rewind/pkg/typefaces"
)

// Tomorrow is the Stitch kicker/subtitle typeface (static/css font-mono).
// UnifrakturCook-Bold is OFL blackletter for chapter titles.
//
//go:embed fonts/Tomorrow-Bold.ttf fonts/Tomorrow-Regular.ttf fonts/UnifrakturCook-Bold.ttf
var titleFontFS embed.FS

var titleFontOnce sync.Once
var titleFontBold string
var titleFontRegular string
var titleFontGothic string
var titleFontErr error

// TitleFontPaths extracts the bundled Tomorrow TTF files to a temp dir so
// ffmpeg drawtext can load them via fontfile=. The encoder image has no
// system fonts; without this, title cards render in a tiny bitmap fallback
// and drop punctuation.
func TitleFontPaths() (bold, regular string, err error) {
	titleFontOnce.Do(extractTitleFonts)
	return titleFontBold, titleFontRegular, titleFontErr
}

// TitleGothicPath is UnifrakturCook Bold for chapter-card mains. Empty if missing.
func TitleGothicPath() string {
	titleFontOnce.Do(extractTitleFonts)
	return titleFontGothic
}

func extractTitleFonts() {
	var dir string
	dir, titleFontErr = titleFontDir()
	if titleFontErr != nil {
		return
	}
	titleFontBold, titleFontErr = extractTitleFont(dir, "Tomorrow-Bold.ttf")
	if titleFontErr != nil {
		return
	}
	titleFontRegular, titleFontErr = extractTitleFont(dir, "Tomorrow-Regular.ttf")
	if titleFontErr != nil {
		return
	}
	titleFontGothic, _ = extractTitleFont(dir, "UnifrakturCook-Bold.ttf")
}

func extractTitleFont(dir, name string) (string, error) {
	dest := filepath.Join(dir, name)
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		return dest, nil
	}
	b, err := titleFontFS.ReadFile("fonts/" + name)
	if err != nil {
		return "", err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dest, nil
}

func titleCardFontFiles(family string) (main, regular string) {
	bold, bundledRegular, _ := TitleFontPaths()
	gothic := TitleGothicPath()
	regular = bundledRegular
	main = gothic
	if main == "" {
		main = bold
	}
	family = strings.TrimSpace(family)
	if family == "" {
		return main, regular
	}
	switch strings.ToLower(strings.ReplaceAll(family, " ", "")) {
	case "unifrakturcook", "unifraktur-cook":
		return main, regular
	case "tomorrow":
		return bold, bundledRegular
	}
	if p := typefaces.File(family, true); p != "" {
		main = p
	}
	if p := typefaces.File(family, false); p != "" {
		regular = p
	}
	return main, regular
}

func titleFontDir() (string, error) {
	dir := filepath.Join(os.TempDir(), "rewind-fonts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// writeTitleTextFile writes drawtext source to a temp file so punctuation
// (apostrophes, ampersands, colons) never has to be escaped inside lavfi.
func writeTitleTextFile(text string) (string, error) {
	dir, err := titleFontDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(text))
	dest := filepath.Join(dir, fmt.Sprintf("t-%x.txt", sum[:8]))
	if st, err := os.Stat(dest); err == nil && st.Size() == int64(len(text)) {
		return dest, nil
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dest, nil
}
