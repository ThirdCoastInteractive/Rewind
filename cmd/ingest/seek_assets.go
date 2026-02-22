package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

type seekLevelSpec struct {
	Name            string  `json:"name"`
	IntervalSeconds float64 `json:"interval_seconds"`
	ThumbWidth      int     `json:"thumb_width"`
	ThumbHeight     int     `json:"thumb_height"`
	Cols            int     `json:"cols"`
	Rows            int     `json:"rows"`
	VTTPath         string  `json:"vtt_path"`
}

type seekManifest struct {
	Format string          `json:"format"`
	Levels []seekLevelSpec `json:"levels"`
}

var (
	seekFormatV1     = "rewind-seek-v1"
	waveformFormatV1 = "rewind-waveform-v1" // referenced by other file
	reSeekLevelSafe  = regexp.MustCompile(`^[a-z0-9_-]+$`)
	seekBaseLevels   = []seekLevelSpec{
		{
			Name:            "coarse",
			IntervalSeconds: 30,
			ThumbWidth:      96,
			ThumbHeight:     54,
			Cols:            12,
			Rows:            10,
			VTTPath:         "levels/coarse/seek.vtt",
		},
		{
			Name:            "medium",
			IntervalSeconds: 10,
			ThumbWidth:      160,
			ThumbHeight:     90,
			Cols:            10,
			Rows:            10,
			VTTPath:         "levels/medium/seek.vtt",
		},
		{
			Name:            "fine",
			IntervalSeconds: 1,
			ThumbWidth:      160,
			ThumbHeight:     90,
			Cols:            10,
			Rows:            10,
			VTTPath:         "levels/fine/seek.vtt",
		},
	}
)

func seekLevelsFromEnv() []seekLevelSpec {
	levels := make([]seekLevelSpec, 0, 3)
	levels = append(levels, seekBaseLevels...)
	if v := strings.TrimSpace(os.Getenv("SEEK_ENABLE_XFINE")); v == "1" || strings.EqualFold(v, "true") {
		levels = append(levels, seekLevelSpec{
			Name:            "x-fine",
			IntervalSeconds: 0.5,
			ThumbWidth:      120,
			ThumbHeight:     68,
			Cols:            12,
			Rows:            10,
			VTTPath:         "levels/x-fine/seek.vtt",
		})
	}
	if v := strings.TrimSpace(os.Getenv("SEEK_ENABLE_XXFINE")); v == "1" || strings.EqualFold(v, "true") {
		levels = append(levels, seekLevelSpec{
			Name:            "xx-fine",
			IntervalSeconds: 0.25,
			ThumbWidth:      96,
			ThumbHeight:     54,
			Cols:            12,
			Rows:            10,
			VTTPath:         "levels/xx-fine/seek.vtt",
		})
	}
	if v := strings.TrimSpace(os.Getenv("SEEK_ENABLE_XXXFINE")); v == "1" || strings.EqualFold(v, "true") {
		levels = append(levels, seekLevelSpec{
			Name:            "xxx-fine",
			IntervalSeconds: 0.1,
			ThumbWidth:      80,
			ThumbHeight:     45,
			Cols:            12,
			Rows:            10,
			VTTPath:         "levels/xxx-fine/seek.vtt",
		})
	}
	return levels
}

func seekDirForVideoPath(videoPath string) (string, error) {
	videoPath = strings.TrimSpace(videoPath)
	if videoPath == "" {
		return "", errors.New("missing video path")
	}
	return filepath.Join(filepath.Dir(videoPath), "seek"), nil
}

func loadSeekManifest(path string) (*seekManifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m seekManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if strings.TrimSpace(m.Format) == "" {
		return nil, errors.New("missing format")
	}
	return &m, nil
}

func writeSeekManifest(path string, levels []seekLevelSpec) error {
	m := seekManifest{Format: seekFormatV1, Levels: levels}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

// verifySeekAssetsDetailed checks which seek levels exist and are valid.
// Returns a map of level name -> exists, or nil if no manifest exists.
func verifySeekAssetsDetailed(videoPath string) (map[string]bool, error) {
	seekDir, err := seekDirForVideoPath(videoPath)
	if err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(seekDir, "seek.json")
	manifest, err := loadSeekManifest(manifestPath)
	if err != nil {
		return nil, err
	}

	levels := make(map[string]bool)
	for _, lvl := range manifest.Levels {
		vttPath := filepath.Join(seekDir, filepath.FromSlash(lvl.VTTPath))
		levelDir := filepath.Dir(vttPath)
		firstSheet := filepath.Join(levelDir, "seek-000.jpg")

		// Level is valid if both VTT and first sprite sheet exist
		vttExists := false
		sheetExists := false
		if _, err := os.Stat(vttPath); err == nil {
			vttExists = true
		}
		if _, err := os.Stat(firstSheet); err == nil {
			sheetExists = true
		}

		levels[lvl.Name] = vttExists && sheetExists
	}

	return levels, nil
}

func ensureSeekAssets(ctx context.Context, videoPath string, durationSeconds *int32) (bool, error) {
	seekDir, err := seekDirForVideoPath(videoPath)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(seekDir, 0755); err != nil {
		return false, fmt.Errorf("mkdir seek dir: %w", err)
	}

	manifestPath := filepath.Join(seekDir, "seek.json")
	levels := seekLevelsFromEnv()

	// Check which levels currently exist
	existingLevels := make(map[string]bool)
	if m, err := loadSeekManifest(manifestPath); err == nil && m.Format == seekFormatV1 {
		// Manifest exists and is correct format - check individual levels
		detailed, err := verifySeekAssetsDetailed(videoPath)
		if err == nil {
			existingLevels = detailed
		}
	}

	// Determine which levels need to be generated
	levelsToGenerate := []seekLevelSpec{}
	for _, lvl := range levels {
		if !existingLevels[lvl.Name] {
			levelsToGenerate = append(levelsToGenerate, lvl)
		}
	}

	// If all levels exist, we're done
	if len(levelsToGenerate) == 0 {
		return false, nil
	}

	dur, err := resolveDurationSeconds(ctx, videoPath, durationSeconds)
	if err != nil {
		return false, err
	}

	// Generate only missing levels (incremental approach)
	for _, lvl := range levelsToGenerate {
		if !reSeekLevelSafe.MatchString(lvl.Name) {
			return false, fmt.Errorf("invalid seek level name: %q", lvl.Name)
		}
		if lvl.IntervalSeconds <= 0 || lvl.ThumbWidth <= 0 || lvl.ThumbHeight <= 0 || lvl.Cols <= 0 || lvl.Rows <= 0 {
			return false, fmt.Errorf("invalid seek level: %+v", lvl)
		}

		vttAbs := filepath.Join(seekDir, filepath.FromSlash(lvl.VTTPath))
		levelDir := filepath.Dir(vttAbs)
		if err := os.MkdirAll(levelDir, 0755); err != nil {
			return false, fmt.Errorf("mkdir seek level dir: %w", err)
		}

		// Remove stale outputs for this level.
		_ = os.Remove(vttAbs)
		if matches, _ := filepath.Glob(filepath.Join(levelDir, "seek-*.jpg")); len(matches) > 0 {
			for _, p := range matches {
				_ = os.Remove(p)
			}
		}

		// Generate sheets
		pattern := filepath.Join(levelDir, "seek-%03d.jpg")
		if err := runFFmpegSeekSheets(ctx, videoPath, lvl, pattern); err != nil {
			slog.Warn("seek sheet generation failed", "video", videoPath, "level", lvl.Name, "error", err)
			continue
		}

		// Generate VTT mapping
		if err := writeSeekVTT(vttAbs, lvl, dur); err != nil {
			slog.Warn("seek vtt generation failed", "video", videoPath, "level", lvl.Name, "error", err)
			continue
		}

		// Tiny pause between levels to reduce bursty CPU.
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}

	if err := writeSeekManifest(manifestPath, levels); err != nil {
		return true, fmt.Errorf("write seek manifest: %w", err)
	}

	return true, nil
}

func runFFmpegSeekSheets(ctx context.Context, videoPath string, lvl seekLevelSpec, outPattern string) error {
	// Build complex filter chain: fps → scale → crop → tile
	// Note: Scale with force_original_aspect_ratio=increase + crop ensures exact dimensions
	vf := fmt.Sprintf(
		"fps=1/%g,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,tile=%dx%d",
		lvl.IntervalSeconds,
		lvl.ThumbWidth,
		lvl.ThumbHeight,
		lvl.ThumbWidth,
		lvl.ThumbHeight,
		lvl.Cols,
		lvl.Rows,
	)

	result := ffmpeg.RunCapture(ctx, videoPath, outPattern,
		ffmpeg.Filter(vf),
		ffmpeg.Quality(4),
		ffmpeg.ExtraArgs("-start_number", "0"),
	)
	if result.Logs != "" {
		slog.Info("ffmpeg seek sheet output", "level", lvl.Name, "logs", result.Logs)
	}
	return result.Err
}

func writeSeekVTT(path string, lvl seekLevelSpec, durationSeconds float64) error {
	if durationSeconds <= 0 {
		return errors.New("invalid duration")
	}
	interval := float64(lvl.IntervalSeconds)
	if interval <= 0 {
		return errors.New("invalid interval")
	}
	framesPerSheet := float64(lvl.Cols * lvl.Rows)
	if framesPerSheet <= 0 {
		return errors.New("invalid grid")
	}

	cueCount := int(durationSeconds / interval)
	if durationSeconds-interval*float64(cueCount) > 0.0001 {
		cueCount++
	}
	if cueCount < 1 {
		cueCount = 1
	}

	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	b.WriteString("NOTE ")
	b.WriteString(seekFormatV1)
	b.WriteString(" interval=")
	b.WriteString(strconv.FormatFloat(lvl.IntervalSeconds, 'f', -1, 64))
	b.WriteString("s size=")
	b.WriteString(strconv.Itoa(lvl.ThumbWidth))
	b.WriteString("x")
	b.WriteString(strconv.Itoa(lvl.ThumbHeight))
	b.WriteString(" grid=")
	b.WriteString(strconv.Itoa(lvl.Cols))
	b.WriteString("x")
	b.WriteString(strconv.Itoa(lvl.Rows))
	b.WriteString("\n\n")

	for i := 0; i < cueCount; i++ {
		start := float64(i) * interval
		end := start + interval
		if end > durationSeconds {
			end = durationSeconds
		}
		sheetIndex := int(float64(i) / framesPerSheet)
		cell := i % int(framesPerSheet)
		xCell := cell % lvl.Cols
		yCell := cell / lvl.Cols
		x := xCell * lvl.ThumbWidth
		y := yCell * lvl.ThumbHeight

		b.WriteString(formatVTTTime(start))
		b.WriteString(" --> ")
		b.WriteString(formatVTTTime(end))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("seek-%03d.jpg#xywh=%d,%d,%d,%d\n\n", sheetIndex, x, y, lvl.ThumbWidth, lvl.ThumbHeight))
	}

	return os.WriteFile(path, []byte(b.String()), 0644)
}

func formatVTTTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	totalMillis := int64(seconds * 1000)
	h := totalMillis / (3600 * 1000)
	m := (totalMillis / (60 * 1000)) % 60
	s := (totalMillis / 1000) % 60
	ms := totalMillis % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

func resolveDurationSeconds(ctx context.Context, videoPath string, durationSeconds *int32) (float64, error) {
	if durationSeconds != nil && *durationSeconds > 0 {
		return float64(*durationSeconds), nil
	}
	// Fallback to ffprobe.
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration: %w", err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0, errors.New("ffprobe returned empty duration")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse ffprobe duration: %w", err)
	}
	if v <= 0 {
		return 0, errors.New("invalid ffprobe duration")
	}
	return v, nil
}
