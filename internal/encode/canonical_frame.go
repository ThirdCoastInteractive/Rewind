package encode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

func processCanonicalFrame(ctx context.Context, q *db.Queries, exportsDir, downloadsDir string, job *db.FindAndLockPendingStitchJobRow) error {
	var snap stitch.RenderSnapshot
	if err := json.Unmarshal(job.DocumentSnapshot, &snap); err != nil {
		return fmt.Errorf("invalid canonical frame snapshot: %w", err)
	}
	timeUS := snap.Options.FrameTimeUS
	if timeUS <= 0 && job.FrameTimeUs != nil {
		timeUS = *job.FrameTimeUs
	}
	if timeUS < 0 {
		return fmt.Errorf("invalid frame timestamp")
	}
	// Render the complete canonical sequence first, preserving composition layers and frozen trims.
	snap.Options.Scope, snap.Options.FrameTimeUS = "all", 0
	full, _ := json.Marshal(snap)
	sequenceJob := *job
	sequenceJob.DocumentSnapshot = full
	ext := ".mp4"
	if job.Format == "webm" {
		ext = ".webm"
	}
	sequence := filepath.Join(exportsDir, "stitch", uuidString(job.ID)+ext)
	var frameOutput string
	if err := processCanonicalStitchFinalize(ctx, q, exportsDir, downloadsDir, &sequenceJob, false, func(input string) error {
		ffmpegPath, err := exec.LookPath("ffmpeg")
		if err != nil {
			ffmpegPath = `C:\bin\ffmpeg.exe`
		}
		if _, err := os.Stat(ffmpegPath); err != nil {
			return err
		}
		dir := filepath.Join(exportsDir, "stitch")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		frameOutput = filepath.Join(dir, uuidString(job.ID)+".png")
		args := []string{"-hide_banner", "-loglevel", "error", "-y", "-ss", fmt.Sprintf("%.6f", float64(timeUS)/1e6), "-i", input, "-frames:v", "1"}
		if snap.Document.Width > 0 && snap.Document.Height > 0 {
			args = append(args, "-vf", fmt.Sprintf("scale=%d:%d", snap.Document.Width, snap.Document.Height))
		}
		args = append(args, frameOutput)
		if out, err := exec.CommandContext(ctx, ffmpegPath, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("frame extraction: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		return nil
	}); err != nil {
		return err
	}
	if frameOutput == "" {
		return fmt.Errorf("frame extraction produced no output")
	}
	_ = os.Remove(sequence)
	st, err := os.Stat(frameOutput)
	if err != nil {
		return err
	}
	return q.FinishStitchJobReady(ctx, &db.FinishStitchJobReadyParams{ID: job.ID, FilePath: frameOutput, SizeBytes: st.Size()})
}
