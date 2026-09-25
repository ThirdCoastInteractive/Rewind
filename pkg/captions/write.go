package captions

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CuesFromStoredTranscript recovers timed cues from the normalized JSON first,
// then from a raw VTT fallback. It is the bridge between searchable database
// transcripts and the sidecar file consumed by the browser player.
func CuesFromStoredTranscript(cuesJSON []byte, raw string) ([]Cue, error) {
	var cues []Cue
	if len(cuesJSON) > 0 && string(cuesJSON) != "null" {
		if err := json.Unmarshal(cuesJSON, &cues); err == nil && len(cues) > 0 {
			return ReadableLines(cues), nil
		}
	}
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("stored transcript has no timed cues")
	}
	doc, err := ParseString(raw)
	if err != nil {
		return nil, fmt.Errorf("parse stored transcript: %w", err)
	}
	if len(doc.Cues) == 0 {
		return nil, fmt.Errorf("stored transcript has no timed cues")
	}
	return ReadableLines(doc.Cues), nil
}

// WriteVTTFile atomically writes a canonical WebVTT sidecar.
func WriteVTTFile(path string, cues []Cue) error {
	if len(cues) == 0 {
		return fmt.Errorf("cannot write empty captions")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".captions-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := WriteVTT(tmp, cues); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// WriteVTT writes a clean WebVTT for the given cues.
func WriteVTT(w io.Writer, cues []Cue) error {
	if _, err := io.WriteString(w, "WEBVTT\n\n"); err != nil {
		return err
	}
	for _, c := range cues {
		line := fmt.Sprintf("%s --> %s\n%s\n\n", formatVTTTime(c.Start), formatVTTTime(c.End), c.Text)
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
	}
	return nil
}

func formatVTTTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	d := time.Duration(seconds * float64(time.Second))
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	d -= s * time.Second
	ms := d / time.Millisecond
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

// LooksDirty reports whether raw VTT bytes still have YouTube auto-caption
// markup that CleanFile would strip.
func LooksDirty(raw []byte) bool {
	s := string(raw)
	if karaokeRe.MatchString(s) {
		return true
	}
	if cueSettingRe.MatchString(s) {
		return true
	}
	if strings.Contains(s, "&gt;") || strings.Contains(s, "&amp;") || strings.Contains(s, "&nbsp;") {
		return true
	}
	return false
}

// CleanFile parses path, and if the VTT is dirty writes a cleaned canonical
// file. The original is copied to a sibling `.src.vtt` once (never deleted).
// Returns the cleaned document even when the file was already clean.
func CleanFile(path string) (*Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := ParseString(string(raw))
	if err != nil {
		return nil, err
	}
	if !doc.Dirty && !LooksDirty(raw) {
		return doc, nil
	}
	srcPath := srcSidecarPath(path)
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		if err := os.WriteFile(srcPath, raw, 0o644); err != nil {
			return nil, fmt.Errorf("preserve original vtt: %w", err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".captions-clean-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	if err := WriteVTT(tmp, doc.Cues); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return nil, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return nil, err
	}
	return doc, nil
}

func srcSidecarPath(canonical string) string {
	// uuid.captions.en.vtt -> uuid.captions.en.src.vtt
	ext := filepath.Ext(canonical)
	base := strings.TrimSuffix(canonical, ext)
	return base + ".src" + ext
}
