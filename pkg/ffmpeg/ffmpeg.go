// Package ffmpeg provides a composable API for building and executing ffmpeg commands.
package ffmpeg

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Command represents an ffmpeg command being built.
type Command struct {
	input        string
	output       string
	preInput     []string // args before -i (like -ss for input seeking)
	postInput    []string // args after -i
	filters      []string // collected -vf filters
	audioFilters []string // collected -af filters
}

// Option modifies a Command. Options are composable and order-independent
// (ffmpeg will receive args in correct order regardless of option order).
type Option interface {
	Apply(cmd *Command)
}

// OptionFunc is a function that implements Option.
type OptionFunc func(cmd *Command)

// Apply implements Option.
func (f OptionFunc) Apply(cmd *Command) { f(cmd) }

// NewCommand creates a command with input/output and applies options.
func NewCommand(input, output string, opts ...Option) *Command {
	cmd := &Command{
		input:  input,
		output: output,
	}
	for _, opt := range opts {
		opt.Apply(cmd)
	}
	return cmd
}

// Build returns the complete ffmpeg argument list.
func (c *Command) Build() []string {
	args := []string{"-hide_banner", "-y"}

	// Pre-input args (seeking)
	args = append(args, c.preInput...)

	// Input
	args = append(args, "-i", c.input)

	// Post-input args
	args = append(args, c.postInput...)

	// Combine video filters
	if len(c.filters) > 0 {
		args = append(args, "-vf", strings.Join(c.filters, ","))
	}

	// Combine audio filters
	if len(c.audioFilters) > 0 {
		args = append(args, "-af", strings.Join(c.audioFilters, ","))
	}

	// Auto-apply faststart for MP4/M4A outputs
	ext := strings.ToLower(filepath.Ext(c.output))
	if ext == ".mp4" || ext == ".m4a" || ext == ".mov" {
		args = append(args, "-movflags", "+faststart")
	}

	// Output
	args = append(args, c.output)

	return args
}

// Run executes the ffmpeg command.
func (c *Command) Run(ctx context.Context) error {
	return run(ctx, c.Build(), nil)
}

// RunCapture executes the ffmpeg command and returns both stderr logs and any error.
func (c *Command) RunCapture(ctx context.Context) RunResult {
	return runCapture(ctx, c.Build())
}

// RunWithProgress executes with progress reporting.
func (c *Command) RunWithProgress(ctx context.Context, progress chan<- Progress) error {
	// Insert progress flags
	args := c.Build()
	// Find position after -hide_banner -y to insert progress flags
	progressArgs := []string{args[0], args[1], "-progress", "pipe:1", "-nostats"}
	progressArgs = append(progressArgs, args[2:]...)
	return run(ctx, progressArgs, progress)
}

// Start starts the command and returns a Process handle for lifecycle management.
// The caller is responsible for calling Wait() or Kill() to clean up.
func (c *Command) Start(ctx context.Context) (*Process, error) {
	return Start(ctx, c.Build(), nil)
}

// StartWithProgress starts the command with progress reporting.
// The caller is responsible for calling Wait() or Kill() to clean up.
func (c *Command) StartWithProgress(ctx context.Context, progress chan<- Progress) (*Process, error) {
	args := c.Build()
	progressArgs := []string{args[0], args[1], "-progress", "pipe:1", "-nostats"}
	progressArgs = append(progressArgs, args[2:]...)
	return Start(ctx, progressArgs, progress)
}

// Run executes the ffmpeg command with the given options.
func Run(ctx context.Context, input, output string, opts ...Option) error {
	return NewCommand(input, output, opts...).Run(ctx)
}

// RunCapture executes the ffmpeg command and returns both the stderr logs and any error.
func RunCapture(ctx context.Context, input, output string, opts ...Option) RunResult {
	return NewCommand(input, output, opts...).RunCapture(ctx)
}

// RunWithProgress executes and reports progress.
func RunWithProgress(ctx context.Context, input, output string, progress chan<- Progress, opts ...Option) error {
	return NewCommand(input, output, opts...).RunWithProgress(ctx, progress)
}

// --- Seeking Options ---

// Seek sets the start position (input seeking, before -i).
func Seek(start time.Duration) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.preInput = append(cmd.preInput, "-ss", formatDuration(start))
	})
}

// Duration sets the output duration.
func Duration(d time.Duration) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-t", formatDuration(d))
	})
}

// SeekTo sets start position and calculates duration from start to end.
func SeekTo(start, end time.Duration) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.preInput = append(cmd.preInput, "-ss", formatDuration(start))
		duration := end - start
		if duration > 0 {
			cmd.postInput = append(cmd.postInput, "-t", formatDuration(duration))
		}
	})
}

// --- Video Codec Options ---

// VideoCodec sets the video codec (-c:v).
func VideoCodec(codec string) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-c:v", codec)
	})
}

// CRF sets the constant rate factor.
func CRF(value int) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-crf", itoa(value))
	})
}

// Preset sets the encoding preset (ultrafast, fast, medium, etc.).
func Preset(name string) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-preset", name)
	})
}

// PixelFormat sets the pixel format (-pix_fmt).
func PixelFormat(fmt string) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-pix_fmt", fmt)
	})
}

// --- Audio Codec Options ---

// AudioCodec sets the audio codec (-c:a).
func AudioCodec(codec string) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-c:a", codec)
	})
}

// AudioBitrate sets the audio bitrate (-b:a).
func AudioBitrate(bitrate string) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-b:a", bitrate)
	})
}

// AudioChannels sets the number of audio channels (-ac).
func AudioChannels(n int) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-ac", itoa(n))
	})
}

// AudioSampleRate sets the audio sample rate (-ar).
func AudioSampleRate(hz int) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-ar", itoa(hz))
	})
}

// --- Stream Copy Options (variables, not functions) ---

// CopyVideo copies the video stream without re-encoding (-c:v copy).
var CopyVideo Option = OptionFunc(func(cmd *Command) {
	cmd.postInput = append(cmd.postInput, "-c:v", "copy")
})

// CopyAudio copies the audio stream without re-encoding (-c:a copy).
var CopyAudio Option = OptionFunc(func(cmd *Command) {
	cmd.postInput = append(cmd.postInput, "-c:a", "copy")
})

// CopyAll copies all streams without re-encoding (-c copy).
var CopyAll Option = OptionFunc(func(cmd *Command) {
	cmd.postInput = append(cmd.postInput, "-c", "copy")
})

// NoAudio disables audio in output (-an).
var NoAudio Option = OptionFunc(func(cmd *Command) {
	cmd.postInput = append(cmd.postInput, "-an")
})

// MapAll maps all streams from input (-map 0).
var MapAll Option = OptionFunc(func(cmd *Command) {
	cmd.postInput = append(cmd.postInput, "-map", "0")
})

// --- Filter Options ---

// Filter adds a video filter to the filter chain.
func Filter(f string) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.filters = append(cmd.filters, f)
	})
}

// AudioFilter adds an audio filter to the filter chain.
func AudioFilter(f string) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.audioFilters = append(cmd.audioFilters, f)
	})
}

// --- Output Options ---

// Frames sets the number of frames to output (-frames:v).
func Frames(n int) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-frames:v", itoa(n))
	})
}

// Quality sets the output quality for images (-q:v).
func Quality(q int) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-q:v", itoa(q))
	})
}

// --- Metadata ---

// Metadata sets a metadata key-value pair.
func Metadata(key, value string) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-metadata", key+"="+value)
	})
}

// MapStream maps a specific stream (-map {spec}).
func MapStream(spec string) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, "-map", spec)
	})
}

// --- Misc ---

// LogLevel sets the logging level.
func LogLevel(level string) Option {
	return OptionFunc(func(cmd *Command) {
		// Insert at beginning of preInput so it's early in args
		cmd.preInput = append([]string{"-loglevel", level}, cmd.preInput...)
	})
}

// ExtraArgs adds raw arguments (escape hatch for unsupported options).
func ExtraArgs(args ...string) Option {
	return OptionFunc(func(cmd *Command) {
		cmd.postInput = append(cmd.postInput, args...)
	})
}

// --- Utility ---

func formatDuration(d time.Duration) string {
	// Format as seconds with millisecond precision for ffmpeg
	secs := d.Seconds()
	return strconv.FormatFloat(secs, 'f', 3, 64)
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
