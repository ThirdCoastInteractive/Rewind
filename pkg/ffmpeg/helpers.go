package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// PreviewOptions configures preview generation.
type PreviewOptions struct {
	StartOffset time.Duration // Where to start the preview (default: 10s)
	Duration    time.Duration // Length of preview (default: 6s)
	MaxWidth    int           // Maximum width (default: 480)
}

// GeneratePreview creates a short preview clip from a video.
func GeneratePreview(ctx context.Context, input, output string, opts *PreviewOptions) error {
	if opts == nil {
		opts = &PreviewOptions{}
	}
	if opts.StartOffset == 0 {
		opts.StartOffset = 10 * time.Second
	}
	if opts.Duration == 0 {
		opts.Duration = 6 * time.Second
	}
	if opts.MaxWidth == 0 {
		opts.MaxWidth = 480
	}

	return Run(ctx, input, output,
		Seek(opts.StartOffset),
		Duration(opts.Duration),
		ScaleWidth(opts.MaxWidth),
		VideoCodec("libx264"),
		Preset("veryfast"),
		CRF(28),
		PixelFormat("yuv420p"),
		NoAudio,
	)
}

// GeneratePreviewCapture is like GeneratePreview but returns ffmpeg logs.
func GeneratePreviewCapture(ctx context.Context, input, output string, opts *PreviewOptions) RunResult {
	if opts == nil {
		opts = &PreviewOptions{}
	}
	if opts.StartOffset == 0 {
		opts.StartOffset = 10 * time.Second
	}
	if opts.Duration == 0 {
		opts.Duration = 6 * time.Second
	}
	if opts.MaxWidth == 0 {
		opts.MaxWidth = 480
	}

	return RunCapture(ctx, input, output,
		Seek(opts.StartOffset),
		Duration(opts.Duration),
		ScaleWidth(opts.MaxWidth),
		VideoCodec("libx264"),
		Preset("veryfast"),
		CRF(28),
		PixelFormat("yuv420p"),
		NoAudio,
	)
}

// ThumbnailOptions configures thumbnail extraction.
type ThumbnailOptions struct {
	Offset   time.Duration // Where to extract from (default: 5s)
	MaxWidth int           // Maximum width (default: 640)
	Quality  int           // JPEG quality 1-31, lower is better (default: 4)
}

// ExtractThumbnail extracts a single frame as an image.
func ExtractThumbnail(ctx context.Context, input, output string, opts *ThumbnailOptions) error {
	if opts == nil {
		opts = &ThumbnailOptions{}
	}
	if opts.Offset == 0 {
		opts.Offset = 5 * time.Second
	}
	if opts.MaxWidth == 0 {
		opts.MaxWidth = 640
	}
	if opts.Quality == 0 {
		opts.Quality = 4
	}

	return Run(ctx, input, output,
		Seek(opts.Offset),
		ScaleWidth(opts.MaxWidth),
		Frames(1),
		Quality(opts.Quality),
	)
}

// ExtractThumbnailCapture is like ExtractThumbnail but returns ffmpeg logs.
func ExtractThumbnailCapture(ctx context.Context, input, output string, opts *ThumbnailOptions) RunResult {
	if opts == nil {
		opts = &ThumbnailOptions{}
	}
	if opts.Offset == 0 {
		opts.Offset = 5 * time.Second
	}
	if opts.MaxWidth == 0 {
		opts.MaxWidth = 640
	}
	if opts.Quality == 0 {
		opts.Quality = 4
	}

	return RunCapture(ctx, input, output,
		Seek(opts.Offset),
		ScaleWidth(opts.MaxWidth),
		Frames(1),
		Quality(opts.Quality),
	)
}

// ExtractClip extracts a time range from a video.
func ExtractClip(ctx context.Context, input, output string, start, end time.Duration, extraOpts ...Option) error {
	opts := []Option{
		SeekTo(start, end),
	}
	opts = append(opts, PresetExportHQ()...)
	opts = append(opts, PresetExportAAC()...)
	opts = append(opts, extraOpts...)
	return Run(ctx, input, output, opts...)
}

// ExtractClipWithProgress is like ExtractClip but reports progress.
func ExtractClipWithProgress(ctx context.Context, input, output string, start, end time.Duration, progress chan<- Progress, extraOpts ...Option) error {
	opts := []Option{
		SeekTo(start, end),
	}
	opts = append(opts, PresetExportHQ()...)
	opts = append(opts, PresetExportAAC()...)
	opts = append(opts, extraOpts...)
	return RunWithProgress(ctx, input, output, progress, opts...)
}

// RemuxOptions configures remuxing.
type RemuxOptions struct {
	Metadata map[string]string // Metadata key-value pairs to set
}

// Remux copies streams to a new container without re-encoding.
func Remux(ctx context.Context, input, output string, opts *RemuxOptions) error {
	if opts == nil {
		opts = &RemuxOptions{}
	}

	runOpts := []Option{CopyAll, MapAll}
	for k, v := range opts.Metadata {
		runOpts = append(runOpts, Metadata(k, v))
	}

	return Run(ctx, input, output, runOpts...)
}

// MuxVideoAudio combines the video stream(s) from videoInput with the first
// audio stream from audioSource into a single MP4.  All streams are copied
// (no re-encoding).  The output gets +faststart automatically because Run
// applies it for .mp4 outputs.
func MuxVideoAudio(ctx context.Context, videoInput, audioSource, output string) error {
	args := []string{
		"-hide_banner", "-y",
		"-i", videoInput,
		"-i", audioSource,
		"-map", "0:v", // video from first input
		"-map", "1:a:0", // first audio track from second input
		"-c", "copy", // stream copy, no re-encoding
		"-movflags", "+faststart",
		output,
	}
	return run(ctx, args, nil)
}

// WaveformOptions configures waveform peak generation.
type WaveformOptions struct {
	SampleRate int // Output sample rate (default: 8000)
	BucketMS   int // Bucket size in milliseconds (default: 100 = 10 peaks/second)
}

// WaveformResult contains waveform generation results.
type WaveformResult struct {
	PeakCount int     // Number of peaks generated
	Duration  float64 // Duration of audio in seconds
	Logs      string  // ffmpeg stderr output
}

// GenerateWaveformPeaks extracts audio and computes peak amplitudes.
// Output is a binary file of int16 peak values.
func GenerateWaveformPeaks(ctx context.Context, input, output string, opts *WaveformOptions) (*WaveformResult, error) {
	if opts == nil {
		opts = &WaveformOptions{}
	}
	if opts.SampleRate == 0 {
		opts.SampleRate = 8000
	}
	if opts.BucketMS == 0 {
		opts.BucketMS = 100
	}

	// Calculate samples per bucket
	samplesPerBucket := (opts.SampleRate * opts.BucketMS) / 1000

	// Run ffmpeg to extract raw PCM audio
	args := []string{
		"-hide_banner", "-y",
		"-i", input,
		"-vn",      // No video
		"-ac", "1", // Mono
		"-ar", fmt.Sprintf("%d", opts.SampleRate),
		"-f", "s16le", // Raw 16-bit little-endian PCM
		"-acodec", "pcm_s16le",
		"pipe:1", // Output to stdout
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: failed to create stdout pipe: %w", err)
	}

	// Capture stderr for logging and error messages
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg: failed to start: %w", err)
	}

	// Process audio samples into peaks
	outFile, err := os.Create(output)
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("ffmpeg: failed to create output file: %w", err)
	}
	defer outFile.Close()

	reader := bufio.NewReader(stdout)
	sampleBuf := make([]byte, 2) // 16-bit samples
	var peaks []int16
	var currentPeak int16
	var sampleCount int
	var totalSamples int64

	for {
		_, err := reader.Read(sampleBuf)
		if err != nil {
			break
		}

		sample := int16(binary.LittleEndian.Uint16(sampleBuf))
		totalSamples++

		// Track absolute max for this bucket
		if sample < 0 {
			sample = -sample
		}
		if sample > currentPeak {
			currentPeak = sample
		}

		sampleCount++
		if sampleCount >= samplesPerBucket {
			peaks = append(peaks, currentPeak)
			currentPeak = 0
			sampleCount = 0
		}
	}

	// Don't forget the last partial bucket
	if sampleCount > 0 {
		peaks = append(peaks, currentPeak)
	}

	// Write peaks as binary int16
	for _, peak := range peaks {
		if err := binary.Write(outFile, binary.LittleEndian, peak); err != nil {
			return nil, fmt.Errorf("ffmpeg: failed to write peak: %w", err)
		}
	}

	if err := cmd.Wait(); err != nil {
		// Ignore errors if we got data (might be broken pipe on short files)
		if len(peaks) == 0 {
			stderrOutput := stderrBuf.String()
			if stderrOutput != "" {
				return nil, fmt.Errorf("ffmpeg waveform generation failed (no peaks generated): %w [input=%s, stderr=%s]", err, input, stderrOutput)
			}
			return nil, fmt.Errorf("ffmpeg waveform generation failed (no peaks generated): %w [input=%s]", err, input)
		}
	}

	duration := float64(totalSamples) / float64(opts.SampleRate)

	return &WaveformResult{
		PeakCount: len(peaks),
		Duration:  duration,
		Logs:      stderrBuf.String(),
	}, nil
}
