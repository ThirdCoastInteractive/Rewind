package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// PreviewOptions configures preview generation.
type PreviewOptions struct {
	StartOffset time.Duration // Where to start the preview (default: 10s)
	Duration    time.Duration // Length of preview (default: 6s)
	MaxWidth    int           // Maximum width (default: 480)
}

// GeneratePreview creates a short preview clip from a video.
func GeneratePreview(ctx context.Context, input, output string, opts *PreviewOptions) RunResult {
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
func ExtractThumbnail(ctx context.Context, input, output string, opts *ThumbnailOptions) RunResult {
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

// ApplyFaststart remuxes an MP4 file in-place (stream copy, no re-encode) so
// the moov atom is at the beginning of the file.  This lets browsers start
// playback and seek immediately without downloading the whole file first.
// A temporary file is written next to the source and atomically renamed over it.
func ApplyFaststart(ctx context.Context, path string) error {
	tmp := path + ".faststart.tmp"
	args := []string{
		"-hide_banner", "-y",
		"-i", path,
		"-c", "copy",
		"-movflags", "+faststart",
		tmp,
	}
	if err := run(ctx, args, nil); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
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

// NeedsVideoTranscode returns true if the video codec is not natively playable
// in modern browsers and should be re-encoded to H.264.
func NeedsVideoTranscode(probe *ProbeResult) bool {
	if probe == nil {
		return false
	}
	switch probe.VideoCodec {
	case "h264", "h265", "hevc", "vp8", "vp9", "av1":
		// Natively playable video codecs
		return false
	case "":
		// Audio-only file — no video transcode needed
		return false
	default:
		return true
	}
}

// StreamableAudioCodec reports whether an audio codec can be packaged for
// browser playback (direct or HLS) without re-encoding. Codecs like Dolby
// Digital (ac3), Dolby Digital Plus (eac3), DTS, and TrueHD are not playable
// by browsers and must be transcoded to AAC.
func StreamableAudioCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "aac", "mp3", "opus", "vorbis", "flac", "":
		return true
	default:
		return false
	}
}

// NeedsAudioTranscode returns true if the (first) audio codec is not natively
// playable in modern browsers and should be re-encoded to AAC.
func NeedsAudioTranscode(probe *ProbeResult) bool {
	if probe == nil {
		return false
	}
	return !StreamableAudioCodec(probe.AudioCodec)
}

// NeedsTranscode returns true if the video and/or audio codecs are not natively
// playable in modern browsers and should be transcoded to H.264+AAC.
func NeedsTranscode(probe *ProbeResult) bool {
	return NeedsVideoTranscode(probe) || NeedsAudioTranscode(probe)
}

// TranscodeToMP4 re-encodes a video to H.264+AAC in an MP4 container.
// Used for legacy codecs (MPEG-4 ASP, WMV, MPEG-2, etc.) that browsers can't play.
// The output gets faststart automatically via the Command builder.
func TranscodeToMP4(ctx context.Context, input, output string) error {
	return Run(ctx, input, output,
		VideoCodec("libx264"),
		Preset("medium"),
		CRF(20),
		PixelFormat("yuv420p"),
		AudioCodec("aac"),
		AudioBitrate("192k"),
	)
}

// TranscodeToMP4WithProgress is like TranscodeToMP4 but reports progress.
func TranscodeToMP4WithProgress(ctx context.Context, input, output string, progress chan<- Progress) error {
	return RunWithProgress(ctx, input, output, progress,
		VideoCodec("libx264"),
		Preset("medium"),
		CRF(20),
		PixelFormat("yuv420p"),
		AudioCodec("aac"),
		AudioBitrate("192k"),
	)
}

// normalizeOptions builds the ffmpeg options that turn an arbitrary input into a
// single browser-playable MP4: keep the first video stream and all audio
// streams, drop subtitles/data, copy streams that browsers can already play, and
// only re-encode what they can't.
//
//   - Video: copied when the codec is browser-playable (H.264/HEVC/VP8/VP9/AV1);
//     re-encoded to H.264 only for legacy codecs browsers can't play.
//   - Audio: copied when streamable (AAC/MP3/Opus/…); transcoded to AAC otherwise
//     (Dolby Digital/eac3, ac3, DTS, TrueHD, PCM, …). Channel layout is preserved.
//   - Subtitles/data: dropped (captions are written as sidecar .vtt at ingest).
//   - HEVC kept as-is is tagged hvc1 so Safari will decode it.
//
// The .mp4 output gets +faststart automatically via the Command builder.
func normalizeOptions(probe *ProbeResult) []Option {
	opts := []Option{
		MapStream("0:v:0"),
		MapStream("0:a?"),
		ExtraArgs("-sn", "-dn", "-map_metadata", "0"),
	}

	if NeedsVideoTranscode(probe) {
		opts = append(opts, VideoCodec("libx264"), Preset("medium"), CRF(20), PixelFormat("yuv420p"))
	} else {
		opts = append(opts, CopyVideo)
		if probe != nil {
			switch probe.VideoCodec {
			case "hevc", "h265":
				opts = append(opts, ExtraArgs("-tag:v", "hvc1"))
			}
		}
	}

	if NeedsAudioTranscode(probe) {
		opts = append(opts, AudioCodec("aac"), AudioBitrate("192k"))
	} else {
		opts = append(opts, CopyAudio)
	}

	return opts
}

// NormalizeToStreamableMP4 rewrites input into a single browser-playable MP4 at
// output, copying anything browsers already support and only re-encoding what
// they don't (legacy video → H.264, non-streamable audio → AAC). See
// normalizeOptions for the exact stream handling.
func NormalizeToStreamableMP4(ctx context.Context, input, output string) error {
	probe, err := Probe(ctx, input)
	if err != nil {
		return fmt.Errorf("normalize: probe %s: %w", input, err)
	}
	return Run(ctx, input, output, normalizeOptions(probe)...)
}

// NormalizeToStreamableMP4WithProgress is like NormalizeToStreamableMP4 but
// reports progress.
func NormalizeToStreamableMP4WithProgress(ctx context.Context, input, output string, progress chan<- Progress) error {
	probe, err := Probe(ctx, input)
	if err != nil {
		return fmt.Errorf("normalize: probe %s: %w", input, err)
	}
	return RunWithProgress(ctx, input, output, progress, normalizeOptions(probe)...)
}

// IsStreamableMP4 reports whether a probed file is already a browser-playable
// MP4 that needs no normalization — an MP4 container whose video and audio
// codecs are both natively playable. Such files only need a faststart remux.
func IsStreamableMP4(probe *ProbeResult) bool {
	if probe == nil {
		return false
	}
	if !strings.Contains(strings.ToLower(probe.FormatName), "mp4") {
		return false
	}
	return !NeedsVideoTranscode(probe) && !NeedsAudioTranscode(probe)
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
