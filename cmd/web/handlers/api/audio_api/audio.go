package audio_api

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/pkg/audiobeds"
)

// HandleList lists audio beds as JSON.
func HandleList(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}
		return c.JSON(200, audiobeds.List())
	}
}

// HandleJSON lists beds for the stitch editor.
func HandleJSON(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}
		return c.JSON(200, audiobeds.List())
	}
}

// HandleFile streams one bed for preview.
func HandleFile(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}
		p := audiobeds.File(c.Param("id"))
		if p == "" {
			return c.String(404, "not found")
		}
		return c.File(p)
	}
}

// HandleUpload stores an audio file in AUDIO_DIR.
func HandleUpload(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}
		fh, err := c.FormFile("file")
		if err != nil {
			return c.String(400, "file required")
		}
		ext := strings.ToLower(filepath.Ext(fh.Filename))
		switch ext {
		case ".ogg", ".oga", ".mp3", ".wav", ".flac", ".m4a", ".aac", ".opus":
		default:
			return c.String(400, "unsupported audio type")
		}
		if fh.Size > 40<<20 {
			return c.String(400, "file too large")
		}
		src, err := fh.Open()
		if err != nil {
			return err
		}
		defer src.Close()
		root := audiobeds.Dir()
		if err := os.MkdirAll(root, 0o755); err != nil {
			return err
		}
		base := strings.TrimSuffix(filepath.Base(fh.Filename), ext)
		name := fmt.Sprintf("%s%s", slugFile(base), ext)
		dest := filepath.Join(root, name)
		tmp := dest + ".tmp"
		f, err := os.Create(tmp)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(f, io.LimitReader(src, 40<<20))
		closeErr := f.Close()
		if copyErr != nil {
			_ = os.Remove(tmp)
			return copyErr
		}
		if closeErr != nil {
			_ = os.Remove(tmp)
			return closeErr
		}
		if err := os.Rename(tmp, dest); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return c.JSON(200, audiobeds.List())
	}
}

func slugFile(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prev := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prev = false
		default:
			if !prev && b.Len() > 0 {
				b.WriteByte('-')
				prev = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "sting"
	}
	return out
}
