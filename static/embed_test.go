package static

import (
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbeddedAssetsExist(t *testing.T) {
	expected := []string{
		"dist/cut-page.js",
		"dist/datastar.js",
		"dist/fontawesome/all.min.css",
		"dist/main.css",
		"dist/main.js",
		"dist/producer-scene-preview.js",
		"dist/remote-player-background.js",
		"dist/video-player.css",
		"dist/video-player.js",
		"dist/webfonts/fa-brands-400.woff2",
		"dist/webfonts/fa-regular-400.woff2",
		"dist/webfonts/fa-sharp-regular-400.woff2",
		"dist/webfonts/fa-sharp-solid-900.woff2",
		"dist/webfonts/fa-solid-900.woff2",
		"dist/webfonts/fa-v4compatibility.woff2",
		"fonts/woff/Blobmoji.woff2",
		"fonts/woff/orbitron-v35-latin-700.woff2",
		"fonts/woff/orbitron-v35-latin-regular.woff2",
		"fonts/woff/tomorrow-v19-latin-700.woff2",
		"fonts/woff/tomorrow-v19-latin-regular.woff2",
	}

	var got []string
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		// go:embed uses forward slashes regardless of OS.
		if strings.Contains(path, "\\") {
			return &fs.PathError{Op: "walk", Path: path, Err: fs.ErrInvalid}
		}
		if !strings.HasPrefix(path, "dist/") && !strings.HasPrefix(path, "fonts/") {
			return &fs.PathError{Op: "walk", Path: path, Err: fs.ErrPermission}
		}
		if strings.HasSuffix(path, ".map") {
			return &fs.PathError{Op: "walk", Path: path, Err: fs.ErrInvalid}
		}

		got = append(got, path)
		return nil
	})
	require.NoError(t, err)
	sort.Strings(got)

	extra := diffExtra(got, expected)
	missing := diffMissing(got, expected)
	require.Empty(t, extra, "unexpected embedded files")
	require.Empty(t, missing, "missing embedded files")
	require.Equal(t, expected, got)
}

func diffMissing(got, expected []string) []string {
	gotSet := make(map[string]struct{}, len(got))
	for _, p := range got {
		gotSet[p] = struct{}{}
	}
	var missing []string
	for _, p := range expected {
		if _, ok := gotSet[p]; !ok {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	return missing
}

func diffExtra(got, expected []string) []string {
	expectedSet := make(map[string]struct{}, len(expected))
	for _, p := range expected {
		expectedSet[p] = struct{}{}
	}
	var extra []string
	for _, p := range got {
		if _, ok := expectedSet[p]; !ok {
			extra = append(extra, p)
		}
	}
	sort.Strings(extra)
	return extra
}
