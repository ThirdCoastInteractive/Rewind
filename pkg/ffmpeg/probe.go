package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
)

// ProbeResult contains media file metadata.
type ProbeResult struct {
	// Video properties
	Width       int     // Video width in pixels
	Height      int     // Video height in pixels
	FPS         float64 // Frames per second
	VideoCodec  string  // Video codec name (h264, vp9, etc.)
	PixelFormat string  // Pixel format (yuv420p, etc.)

	// Audio properties
	AudioCodec      string // Audio codec name (aac, opus, etc.)
	AudioChannels   int    // Number of audio channels
	AudioSampleRate int    // Audio sample rate in Hz

	// File properties
	Duration   float64 // Duration in seconds
	Bitrate    int64   // Total bitrate in bits per second
	Size       int64   // File size in bytes
	FormatName string  // Container format (mp4, webm, mkv, etc.)

	// Stream counts
	VideoStreams int
	AudioStreams int

	// Raw JSON from ffprobe (complete output)
	RawJSON map[string]any
}

// ffprobeOutput matches ffprobe JSON output structure.
type ffprobeOutput struct {
	Format struct {
		Filename   string `json:"filename"`
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams []struct {
		Index     int    `json:"index"`
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`

		// Video properties
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		RFrameRate  string `json:"r_frame_rate"`
		PixelFormat string `json:"pix_fmt"`

		// Audio properties
		SampleRate    string `json:"sample_rate"`
		Channels      int    `json:"channels"`
		ChannelLayout string `json:"channel_layout"`
	} `json:"streams"`
}

// Probe runs ffprobe on a file and returns metadata.
func Probe(ctx context.Context, path string) (*ProbeResult, error) {
	args := []string{
		"-hide_banner",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	}

	cmd := exec.CommandContext(ctx, "ffprobe", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe: %w: %s", err, stderr.String())
	}

	rawJSON := stdout.Bytes()

	var output ffprobeOutput
	if err := json.Unmarshal(rawJSON, &output); err != nil {
		return nil, fmt.Errorf("ffprobe: failed to parse output: %w", err)
	}

	// Also parse as generic map for RawJSON
	var rawMap map[string]any
	if err := json.Unmarshal(rawJSON, &rawMap); err != nil {
		return nil, fmt.Errorf("ffprobe: failed to parse raw json: %w", err)
	}

	result := &ProbeResult{
		RawJSON: rawMap,
	}

	// Parse format metadata
	if output.Format.Duration != "" {
		result.Duration, _ = strconv.ParseFloat(output.Format.Duration, 64)
	}
	if output.Format.BitRate != "" {
		result.Bitrate, _ = strconv.ParseInt(output.Format.BitRate, 10, 64)
	}
	if output.Format.Size != "" {
		result.Size, _ = strconv.ParseInt(output.Format.Size, 10, 64)
	}
	result.FormatName = output.Format.FormatName

	// Parse streams
	for _, stream := range output.Streams {
		switch stream.CodecType {
		case "video":
			result.VideoStreams++
			// Only take first video stream metadata
			if result.VideoCodec == "" {
				result.Width = stream.Width
				result.Height = stream.Height
				result.VideoCodec = stream.CodecName
				result.PixelFormat = stream.PixelFormat
				result.FPS = parseFrameRate(stream.RFrameRate)
			}

		case "audio":
			result.AudioStreams++
			// Only take first audio stream metadata
			if result.AudioCodec == "" {
				result.AudioCodec = stream.CodecName
				result.AudioChannels = stream.Channels
				if stream.SampleRate != "" {
					result.AudioSampleRate, _ = strconv.Atoi(stream.SampleRate)
				}
			}
		}
	}

	return result, nil
}

// parseFrameRate parses ffprobe frame rate format (e.g., "30/1" or "30000/1001").
func parseFrameRate(rate string) float64 {
	var num, den int
	_, err := fmt.Sscanf(rate, "%d/%d", &num, &den)
	if err != nil || den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

// ProbeDuration is a convenience function that returns just the duration.
func ProbeDuration(ctx context.Context, path string) (float64, error) {
	result, err := Probe(ctx, path)
	if err != nil {
		return 0, err
	}
	return result.Duration, nil
}
