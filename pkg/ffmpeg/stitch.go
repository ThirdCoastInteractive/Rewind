package ffmpeg

import (
	"fmt"
	"math"
	"strings"
	"time"

	"thirdcoast.systems/rewind/pkg/utils/crops"
)

// SegmentType discriminates clip vs generated title card segments.
type SegmentType string

const (
	// SegmentClip indicates a segment sourced from an existing video file.
	SegmentClip SegmentType = "clip"
	// SegmentTitle indicates a generated title card with text over a solid background.
	SegmentTitle SegmentType = "title"
)

// Segment describes one element in a stitch sequence.
type Segment struct {
	Type SegmentType

	// --- Clip fields (Type == "clip") ---
	Input        string        // absolute path to source video file
	Start        time.Duration // clip start offset in source video
	Duration     time.Duration // clip duration
	HasAudio     bool          // false when source has no audio stream
	VideoFilters []string      // per-segment video filter strings
	AudioFilters []string      // per-segment audio filter strings

	// --- Title card fields (Type == "title") ---
	TitleDuration time.Duration // card display duration
	BgColor       string        // background hex color, e.g. "#000000"
	Text          string        // main title text
	Subtitle      string        // optional second line (empty = omit)
	TextColor     string        // hex color for text, e.g. "#ffffff"
	FontSize      int           // base font size in points
	Position      string        // "center", "top-center", "bottom-center"
}

func (s Segment) segDuration() time.Duration {
	if s.Type == SegmentTitle {
		return s.TitleDuration
	}
	return s.Duration
}

// Transition describes the transition INTO a segment from the previous one.
// nil means hard cut.
type Transition struct {
	Type     string        // xfade transition name: "fade", "dissolve", "wipeleft", etc.
	Duration time.Duration // overlap duration
}

// CompileFilterStrings converts a FilterSpec slice into raw ffmpeg video and
// audio filter strings. These are used by the stitch builder to embed per-segment
// filters inline in the filter_complex chain.
func CompileFilterStrings(specs []FilterSpec, clipCrops crops.CropArray) (video, audio []string, err error) {
	opts, err := CompileFilters(specs, clipCrops)
	if err != nil {
		return nil, nil, err
	}
	// Run options against a scratch Command to collect the filter strings.
	scratch := &Command{}
	for _, opt := range opts {
		opt.Apply(scratch)
	}
	return scratch.VideoFilterStrings(), scratch.AudioFilterStrings(), nil
}

// StitchCommand builds a single ffmpeg command that concatenates multiple
// segments with optional xfade transitions using filter_complex.
//
// transitions must have the same length as segments. transitions[0] is always
// ignored (nothing precedes the first segment). A nil entry or zero Duration
// means a hard cut.
//
// opts are applied after the filter_complex (e.g. codec presets).
func StitchCommand(
	segments []Segment,
	transitions []*Transition,
	output string,
	globalVideoFilters, globalAudioFilters []string,
	outputWidth, outputHeight int,
	opts ...Option,
) *Command {
	args := []string{"-hide_banner", "-y"}

	if outputWidth <= 0 {
		outputWidth = 1920
	}
	if outputHeight <= 0 {
		outputHeight = 1080
	}
	// Output frame rate for normalization. All segments are converted to this
	// fps so that xfade transitions work across heterogeneous sources.
	const outputFPS float64 = 30

	// ------------------------------------------------------------------ //
	// 1. Build input list
	// ------------------------------------------------------------------ //
	// segVideoIdx[i] = ffmpeg input index for segment i's video stream.
	// segAudioIdx[i] = ffmpeg input index for segment i's audio stream
	// (only differs from segVideoIdx for title cards which need 2 inputs).
	segVideoIdx := make([]int, len(segments))
	segAudioIdx := make([]int, len(segments))
	nextIdx := 0

	for i, seg := range segments {
		segVideoIdx[i] = nextIdx
		if seg.Type == SegmentTitle {
			segAudioIdx[i] = nextIdx + 1

			bgColor := seg.BgColor
			if bgColor == "" {
				bgColor = "black"
			}
			textColor := seg.TextColor
			if textColor == "" {
				textColor = "white"
			}
			fontSize := seg.FontSize
			if fontSize <= 0 {
				fontSize = 72
			}
			dur := seg.TitleDuration.Seconds()

			// Build lavfi color+drawtext chain for the video input.
			// Generate at output FPS so fps filter is a no-op and duration is frame-accurate.
			lavfi := fmt.Sprintf("color=c=%s:s=%dx%d:d=%.6f:r=%d", bgColor, outputWidth, outputHeight, dur, int(outputFPS))
			if seg.Text != "" {
				mainX, mainY := titleTextPosition(seg.Position, seg.Subtitle != "", fontSize)
				lavfi += fmt.Sprintf(",drawtext=text='%s':fontsize=%d:fontcolor=%s:x=%s:y=%s",
					escapeDrawtext(seg.Text), fontSize, textColor, mainX, mainY)
			}
			if seg.Subtitle != "" {
				subSize := fontSize / 2
				if subSize < 12 {
					subSize = 12
				}
				_, subY := titleTextPosition(seg.Position, false, fontSize)
				lavfi += fmt.Sprintf(",drawtext=text='%s':fontsize=%d:fontcolor=%s:x=(w-text_w)/2:y=%s+%d+10",
					escapeDrawtext(seg.Subtitle), subSize, textColor, subY, fontSize)
			}

			args = append(args, "-f", "lavfi", "-i", lavfi)
			// Silent audio input; trimmed to card duration in filter_complex.
			args = append(args, "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo")
			nextIdx += 2
		} else {
			if seg.HasAudio {
				segAudioIdx[i] = nextIdx
				args = append(args,
					"-ss", formatDuration(seg.Start),
					"-t", formatDuration(seg.Duration),
					"-i", seg.Input,
				)
				nextIdx++
			} else {
				segAudioIdx[i] = nextIdx + 1
				args = append(args,
					"-ss", formatDuration(seg.Start),
					"-t", formatDuration(seg.Duration),
					"-i", seg.Input,
				)
				args = append(args, "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo")
				nextIdx += 2
			}
		}
	}

	// ------------------------------------------------------------------ //
	// 2. Build filter_complex
	// ------------------------------------------------------------------ //
	var chains []string

	// Normalization filters to ensure all segments have matching properties
	// for xfade compatibility (resolution, fps, pixel format, SAR).
	// setpts/asetpts reset PTS to 0 — critical for xfade offset accuracy.
	videoNorm := fmt.Sprintf("setpts=PTS-STARTPTS,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,fps=%d,format=yuv420p,setsar=1",
		outputWidth, outputHeight, outputWidth, outputHeight, int(outputFPS))
	audioNorm := "aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo"

	// Pre-compute quantized (frame-accurate) durations for each segment.
	// The fps filter truncates video to whole frames, so xfade offsets must
	// be based on the quantized duration rather than the raw clip duration.
	// Audio is atrim'd to match the quantized video duration for A/V sync.
	qDurs := make([]float64, len(segments))
	for i, seg := range segments {
		raw := seg.segDuration().Seconds()
		frames := math.Floor(raw * outputFPS)
		qDurs[i] = frames / outputFPS
	}

	// Per-segment filter chains → [vN] and [aN]
	for i, seg := range segments {
		vidIdx := segVideoIdx[i]
		audIdx := segAudioIdx[i]

		if seg.Type == SegmentTitle {
			// Title cards: generate at output FPS natively so fps filter is a no-op.
			// Atrim silent audio to quantized duration.
			chains = append(chains,
				fmt.Sprintf("[%d:v]fps=%d,format=yuv420p,setsar=1[v%d]", vidIdx, int(outputFPS), i),
				fmt.Sprintf("[%d:a]atrim=0:%.6f,asetpts=PTS-STARTPTS,%s[a%d]", audIdx, qDurs[i], audioNorm, i),
			)
		} else {
			// Build video chain: normalize + per-segment filters
			vFilters := []string{videoNorm}
			vFilters = append(vFilters, seg.VideoFilters...)
			chains = append(chains,
				fmt.Sprintf("[%d:v]%s[v%d]", vidIdx, strings.Join(vFilters, ","), i),
			)
			// Build audio chain: atrim to quantized video duration + normalize + per-segment filters.
			// The atrim ensures audio duration matches the fps-quantized video duration.
			aFilters := []string{
				fmt.Sprintf("atrim=end=%.6f", qDurs[i]),
				"asetpts=PTS-STARTPTS",
				audioNorm,
			}
			aFilters = append(aFilters, seg.AudioFilters...)
			chains = append(chains,
				fmt.Sprintf("[%d:a]%s[a%d]", audIdx, strings.Join(aFilters, ","), i),
			)
		}
	}

	// Pairwise xfade / acrossfade chain.
	// Uses quantized (frame-accurate) durations for offset calculations.
	prevV := "v0"
	prevA := "a0"
	accDur := qDurs[0]

	for i := 1; i < len(segments); i++ {
		nextV := fmt.Sprintf("v%d", i)
		nextA := fmt.Sprintf("a%d", i)
		outV := fmt.Sprintf("xv%d", i)
		outA := fmt.Sprintf("xa%d", i)

		tr := transitions[i]
		var trType string
		var trDurSec float64
		if tr != nil && tr.Duration > 0 {
			trType = tr.Type
			trDurSec = tr.Duration.Seconds()
		} else {
			// Hard cut: use two-frame xfade to keep chain uniform.
			// xfade in FFmpeg 5.x requires ≥2 frames of overlap; single-frame
			// durations silently drop the second input.
			trType = "fade"
			trDurSec = 2.0 / outputFPS
		}

		offset := accDur - trDurSec

		chains = append(chains,
			fmt.Sprintf("[%s][%s]xfade=transition=%s:duration=%.6f:offset=%.6f[%s]",
				prevV, nextV, trType, trDurSec, offset, outV),
			fmt.Sprintf("[%s][%s]acrossfade=d=%.6f[%s]",
				prevA, nextA, trDurSec, outA),
		)

		// Always subtract transition duration from accumulated total,
		// since xfade output = A_dur + B_dur - transition_dur.
		accDur = accDur + qDurs[i] - trDurSec

		prevV = outV
		prevA = outA
	}

	// prevV / prevA now point to the final combined stream labels.
	// Apply global filters if any.
	finalV := prevV
	finalA := prevA

	if len(globalVideoFilters) > 0 {
		chains = append(chains,
			fmt.Sprintf("[%s]%s[finalv]", prevV, strings.Join(globalVideoFilters, ",")),
		)
		finalV = "finalv"
	}
	if len(globalAudioFilters) > 0 {
		chains = append(chains,
			fmt.Sprintf("[%s]%s[finala]", prevA, strings.Join(globalAudioFilters, ",")),
		)
		finalA = "finala"
	}

	filterComplex := strings.Join(chains, ";\n    ")
	args = append(args, "-filter_complex", filterComplex)

	// Map final streams
	args = append(args, "-map", "["+finalV+"]", "-map", "["+finalA+"]")

	// Apply codec/quality options
	scratch := &Command{}
	for _, opt := range opts {
		opt.Apply(scratch)
	}
	args = append(args, scratch.postInput...)

	// movflags for mp4/mov
	if strings.HasSuffix(strings.ToLower(output), ".mp4") ||
		strings.HasSuffix(strings.ToLower(output), ".mov") {
		args = append(args, "-movflags", "+faststart")
	}

	args = append(args, output)
	return &Command{rawArgs: args}
}

// titleTextPosition returns x/y drawtext expressions for title card text.
// hasSubtitle shifts the main text upward to leave room for a subtitle.
func titleTextPosition(position string, hasSubtitle bool, fontSize int) (x, y string) {
	x = "(w-text_w)/2"
	shift := ""
	if hasSubtitle {
		shift = fmt.Sprintf("-text_h/2-%d", fontSize/4+10)
	}
	switch position {
	case "top-center":
		y = "30"
		if hasSubtitle {
			y = "30"
		}
	case "bottom-center":
		if hasSubtitle {
			y = fmt.Sprintf("h-text_h*2-%d", fontSize/4+40)
		} else {
			y = "h-text_h-30"
		}
	default: // "center"
		if hasSubtitle {
			y = "(h-text_h)/2" + shift
		} else {
			y = "(h-text_h)/2"
		}
	}
	return x, y
}

// escapeDrawtext escapes special characters for use in a drawtext filter string.
func escapeDrawtext(s string) string {
	s = strings.ReplaceAll(s, `%`, `%%`)
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, `:`, `\:`)
	return s
}
