package ffmpeg

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var keepFiles = flag.Bool("keep", false, "keep generated test files for inspection")

func TestCommandBuild(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		output   string
		opts     []Option
		wantArgs []string
	}{
		{
			name:   "simple copy",
			input:  "input.mkv",
			output: "output.mp4",
			opts:   []Option{CopyAll},
			wantArgs: []string{
				"-hide_banner", "-y",
				"-i", "input.mkv",
				"-c", "copy",
				"-movflags", "+faststart",
				"output.mp4",
			},
		},
		{
			name:   "seek and duration",
			input:  "input.mp4",
			output: "output.mp4",
			opts: []Option{
				Seek(10 * time.Second),
				Duration(5 * time.Second),
				CopyAll,
			},
			wantArgs: []string{
				"-hide_banner", "-y",
				"-ss", "10.000",
				"-i", "input.mp4",
				"-t", "5.000",
				"-c", "copy",
				"-movflags", "+faststart",
				"output.mp4",
			},
		},
		{
			name:   "seekto calculates duration",
			input:  "input.mp4",
			output: "output.mp4",
			opts: []Option{
				SeekTo(10*time.Second, 25*time.Second),
				CopyAll,
			},
			wantArgs: []string{
				"-hide_banner", "-y",
				"-ss", "10.000",
				"-i", "input.mp4",
				"-t", "15.000",
				"-c", "copy",
				"-movflags", "+faststart",
				"output.mp4",
			},
		},
		{
			name:   "h264 encoding",
			input:  "input.mp4",
			output: "output.mp4",
			opts: []Option{
				VideoCodec("libx264"),
				CRF(23),
				Preset("fast"),
				PixelFormat("yuv420p"),
				AudioCodec("aac"),
				AudioBitrate("192k"),
			},
			wantArgs: []string{
				"-hide_banner", "-y",
				"-i", "input.mp4",
				"-c:v", "libx264",
				"-crf", "23",
				"-preset", "fast",
				"-pix_fmt", "yuv420p",
				"-c:a", "aac",
				"-b:a", "192k",
				"-movflags", "+faststart",
				"output.mp4",
			},
		},
		{
			name:   "filters are combined",
			input:  "input.mp4",
			output: "output.mp4",
			opts: []Option{
				Scale(1280, -2),
				FPS(30),
				CopyAudio,
			},
			wantArgs: []string{
				"-hide_banner", "-y",
				"-i", "input.mp4",
				"-c:a", "copy",
				"-vf", "scale=1280:-2,fps=30",
				"-movflags", "+faststart",
				"output.mp4",
			},
		},
		{
			name:   "thumbnail extraction",
			input:  "input.mp4",
			output: "thumb.jpg",
			opts: []Option{
				Seek(5 * time.Second),
				ScaleWidth(640),
				Frames(1),
				Quality(4),
			},
			wantArgs: []string{
				"-hide_banner", "-y",
				"-ss", "5.000",
				"-i", "input.mp4",
				"-frames:v", "1",
				"-q:v", "4",
				"-vf", "scale=640:-2",
				"thumb.jpg",
			},
		},
		{
			name:   "no faststart for non-mp4",
			input:  "input.mp4",
			output: "output.webm",
			opts:   []Option{CopyAll},
			wantArgs: []string{
				"-hide_banner", "-y",
				"-i", "input.mp4",
				"-c", "copy",
				"output.webm",
			},
		},
		{
			name:   "metadata",
			input:  "input.mp4",
			output: "output.mp4",
			opts: []Option{
				CopyAll,
				Metadata("title", "My Video"),
				Metadata("artist", "Me"),
			},
			wantArgs: []string{
				"-hide_banner", "-y",
				"-i", "input.mp4",
				"-c", "copy",
				"-metadata", "title=My Video",
				"-metadata", "artist=Me",
				"-movflags", "+faststart",
				"output.mp4",
			},
		},
		{
			name:   "preset bundles",
			input:  "input.mp4",
			output: "output.mp4",
			opts:   Flatten(Preset264Fast(), PresetAAC()),
			wantArgs: []string{
				"-hide_banner", "-y",
				"-i", "input.mp4",
				"-c:v", "libx264",
				"-crf", "23",
				"-preset", "ultrafast",
				"-pix_fmt", "yuv420p",
				"-c:a", "aac",
				"-b:a", "192k",
				"-ac", "2",
				"-movflags", "+faststart",
				"output.mp4",
			},
		},
		{
			name:   "extra args escape hatch",
			input:  "input.mp4",
			output: "output.mp4",
			opts: []Option{
				CopyAll,
				ExtraArgs("-start_number", "0"),
			},
			wantArgs: []string{
				"-hide_banner", "-y",
				"-i", "input.mp4",
				"-c", "copy",
				"-start_number", "0",
				"-movflags", "+faststart",
				"output.mp4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCommand(tt.input, tt.output, tt.opts...)
			got := cmd.Build()
			assert.Equal(t, tt.wantArgs, got)
		})
	}
}

func TestCropFilter(t *testing.T) {
	tests := []struct {
		crop CropFilter
		want string
	}{
		{
			crop: CropFilter{CenterX: 0.5, CenterY: 0.5, Width: 1.0, Height: 1.0},
			want: "crop=iw*1.000000:ih*1.000000:iw*0.000000:ih*0.000000",
		},
		{
			crop: CropFilter{CenterX: 0.5, CenterY: 0.5, Width: 0.5, Height: 0.5},
			want: "crop=iw*0.500000:ih*0.500000:iw*0.250000:ih*0.250000",
		},
	}

	for _, tt := range tests {
		got := tt.crop.String()
		assert.Equal(t, tt.want, got)
	}
}

func TestScaleFilter(t *testing.T) {
	tests := []struct {
		scale ScaleFilter
		want  string
	}{
		{ScaleFilter{Width: 1280, Height: 720}, "scale=1280:720"},
		{ScaleFilter{Width: 640, Height: -2}, "scale=640:-2"},
		{ScaleFilter{Width: -2, Height: 480}, "scale=-2:480"},
	}

	for _, tt := range tests {
		got := tt.scale.String()
		assert.Equal(t, tt.want, got)
	}
}

func TestProgressParsing(t *testing.T) {
	input := `frame=100
fps=30.5
bitrate=1234.5kbits/s
total_size=12345678
out_time_us=5000000
speed=2.5x
progress=continue`

	parser := NewProgressParser()

	lines := []string{
		"frame=100",
		"fps=30.5",
		"bitrate=1234.5kbits/s",
		"total_size=12345678",
		"out_time_us=5000000",
		"speed=2.5x",
		"progress=continue",
	}

	var complete bool
	for _, line := range lines {
		if parser.ParseLine(line) {
			complete = true
		}
	}

	require.True(t, complete, "Expected complete progress update")

	p := parser.Current()

	assert.Equal(t, int64(100), p.Frame)
	assert.Equal(t, 30.5, p.FPS)
	assert.Equal(t, "1234.5kbits/s", p.Bitrate)
	assert.Equal(t, int64(12345678), p.TotalSize)
	assert.Equal(t, int64(5000000), p.OutTimeUS)
	assert.Equal(t, int64(5000), p.OutTimeMS())
	assert.Equal(t, "2.5x", p.Speed)
	assert.Equal(t, "continue", p.Progress)

	_ = input // silence unused warning
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0.000"},
		{1 * time.Second, "1.000"},
		{1500 * time.Millisecond, "1.500"},
		{90 * time.Second, "90.000"},
		{time.Hour + 30*time.Minute + 45*time.Second + 500*time.Millisecond, "5445.500"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.d)
		assert.Equal(t, tt.want, got)
	}
}

// =============================================================================
// Integration tests - require ffmpeg to be installed
// =============================================================================

// generateTestVideo creates a test video using ffmpeg's testsrc.
// Returns the path to the generated file. Caller must clean up.
func generateTestVideo(t *testing.T, duration time.Duration) string {
	t.Helper()

	var tmpDir string
	if *keepFiles {
		// Put test files in a visible location
		tmpDir = filepath.Join(".", "testdata", "artifacts", t.Name())
		if err := os.MkdirAll(tmpDir, 0755); err != nil {
			t.Fatalf("failed to create test dir: %v", err)
		}
		t.Logf("Keeping test files in: %s", tmpDir)
		t.Cleanup(func() {
			t.Logf("Test files kept at: %s", tmpDir)
		})
	} else {
		tmpDir = t.TempDir()
	}
	output := filepath.Join(tmpDir, "test_input.mp4")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Generate test pattern video with a little melody
	// aevalsrc generates a C major arpeggio (C4-E4-G4-C5) switching every 0.25s
	// Formula: select frequency based on floor(t*4)%4, with envelope decay
	durStr := formatDuration(duration)
	melody := "aevalsrc=" +
		"'sin(2*PI*t*(" +
		"262*(eq(floor(mod(t*4\\,4))\\,0))+" + // C4
		"330*(eq(floor(mod(t*4\\,4))\\,1))+" + // E4
		"392*(eq(floor(mod(t*4\\,4))\\,2))+" + // G4
		"523*(eq(floor(mod(t*4\\,4))\\,3))" + // C5
		"))*exp(-3*mod(t\\,0.25))'" + // decay envelope
		":d=" + durStr + ":s=44100"

	args := []string{
		"-hide_banner", "-y",
		"-f", "lavfi", "-i", "testsrc2=duration=" + durStr + ":size=320x240:rate=30",
		"-f", "lavfi", "-i", melody,
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "28",
		"-c:a", "aac", "-b:a", "64k",
		"-pix_fmt", "yuv420p",
		"-shortest",
		"-movflags", "+faststart",
		output,
	}

	proc, err := Start(ctx, args, nil)
	require.NoError(t, err, "failed to generate test video")

	err = proc.Wait()
	require.NoError(t, err, "failed to generate test video, stderr: %s", proc.Stderr())

	return output
}

func TestIntegration_Run(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	input := generateTestVideo(t, 2*time.Second)
	outputDir := t.TempDir()
	if *keepFiles {
		outputDir = filepath.Dir(input)
	}
	output := filepath.Join(outputDir, "output.mp4")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := Run(ctx, input, output, CopyAll, MapAll)
	require.NoError(t, err)

	// Verify output exists
	info, err := os.Stat(output)
	require.NoError(t, err, "output file not created")
	assert.Greater(t, info.Size(), int64(0), "output file is empty")
}

func TestIntegration_ExtractThumbnail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	input := generateTestVideo(t, 3*time.Second)
	outputDir := t.TempDir()
	if *keepFiles {
		outputDir = filepath.Dir(input)
	}
	output := filepath.Join(outputDir, "thumb.jpg")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := ExtractThumbnail(ctx, input, output, &ThumbnailOptions{
		Offset:   1 * time.Second,
		MaxWidth: 160,
		Quality:  5,
	})
	require.NoError(t, err)

	info, err := os.Stat(output)
	require.NoError(t, err, "thumbnail not created")
	assert.Greater(t, info.Size(), int64(0), "thumbnail is empty")
}

func TestIntegration_ExtractClip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	input := generateTestVideo(t, 5*time.Second)
	outputDir := t.TempDir()
	if *keepFiles {
		outputDir = filepath.Dir(input)
	}
	output := filepath.Join(outputDir, "clip.mp4")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := ExtractClip(ctx, input, output, 1*time.Second, 3*time.Second)
	require.NoError(t, err)

	// Verify duration is approximately 2 seconds
	result, err := Probe(ctx, output)
	require.NoError(t, err)

	assert.InDelta(t, 2.0, result.Duration, 0.5, "clip duration should be ~2.0")
}

func TestIntegration_ProcessLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	input := generateTestVideo(t, 2*time.Second)
	outputDir := t.TempDir()
	if *keepFiles {
		outputDir = filepath.Dir(input)
	}
	output := filepath.Join(outputDir, "output.mp4")

	ctx := context.Background()

	cmd := NewCommand(input, output, CopyAll, MapAll)
	proc, err := cmd.Start(ctx)
	require.NoError(t, err)

	// Verify we got a PID
	assert.NotEqual(t, 0, proc.PID(), "PID should be non-zero")

	t.Logf("Started ffmpeg with PID %d", proc.PID())

	// Wait for completion
	err = proc.Wait()
	require.NoError(t, err)

	// Verify output
	info, err := os.Stat(output)
	require.NoError(t, err, "output file not created")
	assert.Greater(t, info.Size(), int64(0), "output file is empty")
}

func TestIntegration_ProcessKill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	var tmpDir string
	if *keepFiles {
		tmpDir = filepath.Join(".", "testdata", "artifacts", t.Name())
		if err := os.MkdirAll(tmpDir, 0755); err != nil {
			t.Fatalf("failed to create test dir: %v", err)
		}
		t.Logf("Keeping test files in: %s", tmpDir)
	} else {
		tmpDir = t.TempDir()
	}
	output := filepath.Join(tmpDir, "never_finish.mp4")

	ctx := context.Background()

	// Start a long-running encode that we'll kill
	// Generate 60 seconds of video (will take a while to encode)
	args := []string{
		"-hide_banner", "-y",
		"-f", "lavfi", "-i", "testsrc2=duration=60:size=640x480:rate=30",
		"-c:v", "libx264", "-preset", "veryslow", // intentionally slow
		"-pix_fmt", "yuv420p",
		output,
	}

	proc, err := Start(ctx, args, nil)
	require.NoError(t, err)

	pid := proc.PID()
	require.NotEqual(t, 0, pid, "PID should be non-zero")

	t.Logf("Started long-running ffmpeg with PID %d, will kill it", pid)

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Kill it
	err = proc.Kill()
	require.NoError(t, err)

	// Wait should return an error (killed)
	err = proc.Wait()
	assert.Error(t, err, "Wait() should return error after Kill()")
	// Success - process was killed as expected
}

func TestIntegration_ProgressReporting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	input := generateTestVideo(t, 3*time.Second)
	outputDir := t.TempDir()
	if *keepFiles {
		outputDir = filepath.Dir(input)
	}
	output := filepath.Join(outputDir, "output.mp4")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	progress := make(chan Progress, 100)

	var updates []Progress
	done := make(chan struct{})
	go func() {
		for p := range progress {
			updates = append(updates, p)
		}
		close(done)
	}()

	// Re-encode (not just copy) so we get progress updates
	err := RunWithProgress(ctx, input, output, progress,
		VideoCodec("libx264"),
		Preset("ultrafast"),
		CRF(28),
		PixelFormat("yuv420p"),
		AudioCodec("aac"),
		AudioBitrate("64k"),
	)
	require.NoError(t, err)

	<-done

	require.NotEmpty(t, updates, "should receive progress updates")
	t.Logf("received %d progress updates", len(updates))

	// Check that we got meaningful progress
	last := updates[len(updates)-1]
	assert.Equal(t, "end", last.Progress)
	assert.Greater(t, last.Frame, int64(0), "frame count should be > 0")
	t.Logf("final progress: frame=%d, speed=%s, out_time=%.2fs",
		last.Frame, last.Speed, last.OutTimeSeconds())
}

func TestIntegration_Probe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	input := generateTestVideo(t, 2*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := Probe(ctx, input)
	require.NoError(t, err)

	// Check expected values from our test video
	assert.Equal(t, 320, result.Width)
	assert.Equal(t, 240, result.Height)
	assert.InDelta(t, 2.0, result.Duration, 0.5, "duration should be ~2.0")
	assert.InDelta(t, 30.0, result.FPS, 1.0, "fps should be ~30")
	assert.Equal(t, "h264", result.VideoCodec)
	assert.Equal(t, "yuv420p", result.PixelFormat)

	// Audio properties
	assert.Equal(t, "aac", result.AudioCodec)
	assert.Equal(t, 1, result.AudioChannels)
	assert.Equal(t, 44100, result.AudioSampleRate)

	// Stream counts
	assert.Equal(t, 1, result.VideoStreams)
	assert.Equal(t, 1, result.AudioStreams)

	// Format
	assert.Contains(t, result.FormatName, "mp4")
	assert.Greater(t, result.Size, int64(0))
	assert.Greater(t, result.Bitrate, int64(0))

	// Raw JSON should be present
	assert.NotEmpty(t, result.RawJSON)

	// Dump JSON to file for inspection
	if *keepFiles {
		jsonPath := filepath.Join(filepath.Dir(input), "probe.json")
		jsonBytes, err := json.MarshalIndent(result, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(jsonPath, jsonBytes, 0644)
		require.NoError(t, err)
		t.Logf("Probe JSON written to: %s", jsonPath)
	}

	t.Logf("Probe result: %+v", result)
}
