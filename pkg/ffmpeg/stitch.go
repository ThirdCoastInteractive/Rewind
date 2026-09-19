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
	Layout       *Layout       // optional portrait teaser composition
	// Multicam: input-relative shots (0 = this segment's -ss) + crop definitions.
	// When Shots is nonempty, multicam wins over Layout.
	Crops crops.CropArray
	Shots crops.ShotList

	// --- Title card fields (Type == "title") ---
	TitleDuration time.Duration // card display duration
	BgColor       string        // background hex color, e.g. "#000000"
	Text          string        // main title text
	Subtitle      string        // optional second line (empty = omit)
	TextColor     string        // hex color for text, e.g. "#ffffff"
	Font          string        // optional installed family; empty uses gothic/Tomorrow
	FontSize      int           // base font size in points
	Position      string        // "center", "top-center", "bottom-center"
	Audio         string        // optional music-bed path for intro/outro cards
}

// Layout describes a portrait composition without importing the editor model.
type Layout struct {
	Mode  string
	Crops []LayoutCrop
}

// LayoutCrop is a normalized top-left crop rectangle in source coordinates.
type LayoutCrop struct {
	X, Y, Width, Height float64
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
	return stitchCommandFPS(segments, transitions, output, globalVideoFilters, globalAudioFilters, outputWidth, outputHeight, 30, false, opts...)
}

// StitchCommandFPS is StitchCommand with an explicit output frame rate.
// fps <= 0 defaults to 30. Hard cuts concat; timed transitions still xfade.
func StitchCommandFPS(
	segments []Segment,
	transitions []*Transition,
	output string,
	globalVideoFilters, globalAudioFilters []string,
	outputWidth, outputHeight int,
	outputFPS float64,
	opts ...Option,
) *Command {
	return stitchCommandFPS(segments, transitions, output, globalVideoFilters, globalAudioFilters, outputWidth, outputHeight, outputFPS, false, opts...)
}

// StitchCommandFPSStrict preserves hard cuts after an explicit transition.
func StitchCommandFPSStrict(segments []Segment, transitions []*Transition, output string, globalVideoFilters, globalAudioFilters []string, outputWidth, outputHeight int, outputFPS float64, opts ...Option) *Command {
	return stitchCommandFPS(segments, transitions, output, globalVideoFilters, globalAudioFilters, outputWidth, outputHeight, outputFPS, true, opts...)
}

func stitchCommandFPS(
	segments []Segment,
	transitions []*Transition,
	output string,
	globalVideoFilters, globalAudioFilters []string,
	outputWidth, outputHeight int,
	outputFPS float64,
	strictCuts bool,
	opts ...Option,
) *Command {
	args := []string{"-hide_banner", "-y"}

	if outputWidth <= 0 {
		outputWidth = 1920
	}
	if outputHeight <= 0 {
		outputHeight = 1080
	}
	if outputFPS <= 0 {
		outputFPS = 30
	}

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
			dur := seg.TitleDuration.Seconds()
			lavfi := titleCardLavfi(seg, bgColor, textColor, outputWidth, outputHeight, outputFPS, dur)

			args = append(args, "-f", "lavfi", "-i", lavfi)
			args = append(args, titleAudioInput(seg, dur, 48000)...)
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
	fpsExpr := fmt.Sprintf("%g", outputFPS)
	videoNorm := fmt.Sprintf("setpts=PTS-STARTPTS,scale=%d:%d:flags=lanczos+accurate_rnd:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,fps=%s,format=yuv420p,setsar=1",
		outputWidth, outputHeight, outputWidth, outputHeight, fpsExpr)
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
			chains = append(chains,
				fmt.Sprintf("[%d:v]fps=%s,format=yuv420p,setsar=1[v%d]", vidIdx, fpsExpr, i),
				fmt.Sprintf("[%d:a]%s[a%d]", audIdx, titleAudioFilters(qDurs[i]), i),
			)
		} else {
			// Build video chain: multicam camera switches win over teaser layout,
			// then full-frame normalize. Per-segment filters apply to the result.
			if mc := multicamChains(seg.Shots, seg.Crops, vidIdx, i, outputWidth, outputHeight, fpsExpr, outputFPS, seg.VideoFilters); len(mc) > 0 {
				chains = append(chains, mc...)
			} else if seg.Layout != nil {
				chains = append(chains, layoutChains(seg.Layout, vidIdx, i, outputWidth, outputHeight, fpsExpr, seg.VideoFilters)...)
			} else {
				vFilters := []string{videoNorm}
				vFilters = append(vFilters, seg.VideoFilters...)
				chains = append(chains,
					fmt.Sprintf("[%d:v]%s[v%d]", vidIdx, strings.Join(vFilters, ","), i),
				)
			}
			// Build audio chain: atrim to quantized video duration + normalize + per-segment filters.
			// The atrim ensures audio duration matches the fps-quantized video duration.
			// Multicam keeps continuous source audio (no acrossfade) — same as non-multicam stitch.
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

	prevV := "v0"
	prevA := "a0"
	if len(segments) > 1 && stitchHardCuts(transitions, len(segments)) {
		var parts []string
		for i := range segments {
			parts = append(parts, fmt.Sprintf("[v%d][a%d]", i, i))
		}
		chains = append(chains, strings.Join(parts, "")+fmt.Sprintf("concat=n=%d:v=1:a=1[xv][xa]", len(segments)))
		prevV, prevA = "xv", "xa"
	} else {
		accDur := qDurs[0]
		for i := 1; i < len(segments); i++ {
			nextV := fmt.Sprintf("v%d", i)
			nextA := fmt.Sprintf("a%d", i)
			outV := fmt.Sprintf("xv%d", i)
			outA := fmt.Sprintf("xa%d", i)
			var tr *Transition
			if i < len(transitions) {
				tr = transitions[i]
			}
			if strictCuts && (tr == nil || tr.Duration <= 0) {
				chains = append(chains,
					fmt.Sprintf("[%s][%s]concat=n=2:v=1:a=0[%s]", prevV, nextV, outV),
					fmt.Sprintf("[%s][%s]concat=n=2:v=0:a=1[%s]", prevA, nextA, outA),
				)
				accDur += qDurs[i]
				prevV, prevA = outV, outA
				continue
			}
			trType := "fade"
			trDurSec := 2.0 / outputFPS
			if tr != nil && tr.Duration > 0 {
				trType = tr.Type
				trDurSec = tr.Duration.Seconds()
			}
			offset := accDur - trDurSec
			chains = append(chains,
				fmt.Sprintf("[%s][%s]xfade=transition=%s:duration=%.6f:offset=%.6f[%s]",
					prevV, nextV, trType, trDurSec, offset, outV),
				fmt.Sprintf("[%s][%s]acrossfade=d=%.6f[%s]",
					prevA, nextA, trDurSec, outA),
			)
			accDur = accDur + qDurs[i] - trDurSec
			prevV, prevA = outV, outA
		}
	}
	if strictCuts {
		total := 0.0
		for i, d := range qDurs {
			total += d
			if i > 0 && i < len(transitions) && transitions[i] != nil && transitions[i].Duration > 0 {
				total -= transitions[i].Duration.Seconds()
			}
		}
		chains = append(chains,
			fmt.Sprintf("[%s]trim=duration=%.6f,setpts=PTS-STARTPTS[canonicalv]", prevV, total),
			fmt.Sprintf("[%s]atrim=duration=%.6f,asetpts=PTS-STARTPTS[canonicala]", prevA, total),
		)
		prevV, prevA = "canonicalv", "canonicala"
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

// multicamChains builds per-segment camera-switch filter chains for an already
// trimmed stitch input. Shot times must be input-relative (0 = segment -ss).
// Returns nil when no visible shots remain (caller falls back to layout/full-frame).
func multicamChains(shots crops.ShotList, cropList crops.CropArray, input, segment, width, height int, fps string, outputFPS float64, filters []string) []string {
	if len(shots) == 0 || len(cropList) == 0 {
		return nil
	}
	cropMap := make(map[string]crops.Crop, len(cropList))
	for _, c := range cropList {
		cropMap[c.ID] = c
	}
	visible := make(crops.ShotList, 0, len(shots))
	for _, shot := range shots {
		if _, ok := cropMap[shot.CropID]; ok {
			visible = append(visible, shot)
		}
	}
	if len(visible) == 0 {
		return nil
	}

	final := fmt.Sprintf("v%d", segment)
	postCropNorm := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase:flags=lanczos,crop=%d:%d,fps=%s,format=yuv420p,setsar=1",
		width, height, width, height, fps)

	if len(visible) == 1 {
		shot := visible[0]
		cr := cropMap[shot.CropID]
		parts := []string{"setpts=PTS-STARTPTS"}
		if shot.Start > 0 || shot.End > shot.Start {
			parts = append(parts, fmt.Sprintf("trim=start=%.6f:end=%.6f,setpts=PTS-STARTPTS", shot.Start, shot.End))
		}
		if cropFilter := crops.FFmpegCropFilter(cr.X, cr.Y, cr.Width, cr.Height); cropFilter != "" {
			parts = append(parts, cropFilter)
		}
		parts = append(parts, postCropNorm)
		parts = append(parts, filters...)
		return []string{fmt.Sprintf("[%d:v]%s[%s]", input, strings.Join(parts, ","), final)}
	}

	n := len(visible)
	splitLabels := make([]string, n)
	for i := range visible {
		splitLabels[i] = fmt.Sprintf("[mcs%d_%d]", segment, i)
	}
	chains := []string{
		fmt.Sprintf("[%d:v]split=%d%s", input, n, strings.Join(splitLabels, "")),
	}

	qDurs := make([]float64, n)
	for i, shot := range visible {
		raw := shot.End - shot.Start
		frames := math.Floor(raw * outputFPS)
		qDurs[i] = frames / outputFPS

		cr := cropMap[shot.CropID]
		trimChain := fmt.Sprintf("trim=start=%.6f:end=%.6f,setpts=PTS-STARTPTS", shot.Start, shot.End)
		if cropFilter := crops.FFmpegCropFilter(cr.X, cr.Y, cr.Width, cr.Height); cropFilter != "" {
			trimChain += "," + cropFilter
		}
		trimChain += "," + postCropNorm
		chains = append(chains, fmt.Sprintf("[mcs%d_%d]%s[mcv%d_%d]", segment, i, trimChain, segment, i))
	}

	prevV := fmt.Sprintf("mcv%d_0", segment)
	accDur := qDurs[0]
	for i := 1; i < n; i++ {
		nextV := fmt.Sprintf("mcv%d_%d", segment, i)
		outV := fmt.Sprintf("mcx%d_%d", segment, i)
		if i == n-1 && len(filters) == 0 {
			outV = final
		}
		prevShot := visible[i-1]
		trType := "fade"
		trDurSec := 2.0 / outputFPS
		if prevShot.TransitionOut != nil && prevShot.TransitionOut.Duration > 0 {
			if prevShot.TransitionOut.Type != "" {
				trType = prevShot.TransitionOut.Type
			}
			trDurSec = prevShot.TransitionOut.Duration
		}
		offset := accDur - trDurSec
		chains = append(chains,
			fmt.Sprintf("[%s][%s]xfade=transition=%s:duration=%.6f:offset=%.6f[%s]",
				prevV, nextV, trType, trDurSec, offset, outV),
		)
		accDur = accDur + qDurs[i] - trDurSec
		prevV = outV
	}
	if len(filters) > 0 {
		chains = append(chains, fmt.Sprintf("[%s]%s[%s]", prevV, strings.Join(filters, ","), final))
	}
	return chains
}

func layoutChains(layout *Layout, input, segment, width, height int, fps string, filters []string) []string {
	if layout == nil {
		return nil
	}
	final := fmt.Sprintf("v%d", segment)
	finish := func(chain string) string {
		if len(filters) > 0 {
			chain += "," + strings.Join(filters, ",")
		}
		return chain + "[" + final + "]"
	}
	base := fmt.Sprintf("[%d:v]setpts=PTS-STARTPTS", input)
	fill := func(c LayoutCrop, w, h int) string {
		return fmt.Sprintf("crop=iw*%.6f:ih*%.6f:iw*%.6f:ih*%.6f,scale=%d:%d:force_original_aspect_ratio=increase:flags=lanczos,crop=%d:%d", c.Width, c.Height, c.X, c.Y, w, h, w, h)
	}
	normal := fmt.Sprintf("fps=%s,format=yuv420p,setsar=1", fps)
	switch layout.Mode {
	case "single_speaker":
		if len(layout.Crops) != 1 {
			return []string{base + "," + fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,%s", width, height, width, height, normal) + finish("")}
		}
		return []string{base + "," + fill(layout.Crops[0], width, height) + "," + normal + finish("")}
	case "two_speakers":
		if len(layout.Crops) != 2 || height < 2 {
			return []string{base + "," + fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,%s", width, height, width, height, normal) + finish("")}
		}
		srcTop, srcBottom := fmt.Sprintf("lay%dtop", segment), fmt.Sprintf("lay%dbottom", segment)
		top, bottom := fmt.Sprintf("lay%dt", segment), fmt.Sprintf("lay%db", segment)
		return []string{
			base + fmt.Sprintf(",split=2[%s][%s]", srcTop, srcBottom),
			fmt.Sprintf("[%s]%s,%s[%s]", srcTop, fill(layout.Crops[0], width, height/2), normal, top),
			fmt.Sprintf("[%s]%s,%s[%s]", srcBottom, fill(layout.Crops[1], width, height-height/2), normal, bottom),
			fmt.Sprintf("[%s][%s]vstack=inputs=2,%s%s", top, bottom, normal, finish("")),
		}
	case "preserve_scene":
		srcBG, srcFG := fmt.Sprintf("lay%dsbg", segment), fmt.Sprintf("lay%dsfg", segment)
		bg, fg := fmt.Sprintf("lay%dbg", segment), fmt.Sprintf("lay%dfg", segment)
		fgChain := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:flags=lanczos", width, height)
		if len(layout.Crops) == 1 {
			c := layout.Crops[0]
			fgChain = fmt.Sprintf("crop=iw*%.6f:ih*%.6f:iw*%.6f:ih*%.6f,scale=%d:%d:force_original_aspect_ratio=decrease:flags=lanczos", c.Width, c.Height, c.X, c.Y, width, height)
		}
		return []string{
			base + fmt.Sprintf(",split=2[%s][%s]", srcBG, srcFG),
			fmt.Sprintf("[%s]scale=%d:%d:force_original_aspect_ratio=increase:flags=lanczos,crop=%d:%d,boxblur=20:10[%s]", srcBG, width, height, width, height, bg),
			fmt.Sprintf("[%s]%s[%s]", srcFG, fgChain, fg),
			fmt.Sprintf("[%s][%s]overlay=(W-w)/2:(H-h)/2,%s%s", bg, fg, normal, finish("")),
		}
	default:
		return []string{base + "," + fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,%s", width, height, width, height, normal) + finish("")}
	}
}

func titleDrawtext(fontfile, text string, fontsize int, fontcolor, x, y string) string {
	opts := fmt.Sprintf("fontsize=%d:fontcolor=%s:x=%s:y=%s:expansion=none:line_spacing=%d", fontsize, fontcolor, x, y, fontsize/6+4)
	if fontfile != "" {
		opts = "fontfile='" + escapeDrawtextPath(fontfile) + "':" + opts
	}
	if path, err := writeTitleTextFile(text); err == nil {
		return "drawtext=" + opts + ":textfile='" + escapeDrawtextPath(path) + "'"
	}
	return "drawtext=" + opts + ":text='" + escapeDrawtext(text) + "'"
}

func withAlpha(color string, alpha float64) string {
	if color == "" || strings.Contains(color, "@") {
		return color
	}
	return fmt.Sprintf("%s@%.2f", color, alpha)
}

// escapeDrawtext escapes a value wrapped in single quotes inside a lavfi graph.
// ffmpeg-utils: a `'` inside a quoted string is escaped with a backslash.
func escapeDrawtext(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `%%`)
	s = strings.ReplaceAll(s, `:`, `\:`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

func escapeDrawtextPath(p string) string {
	p = strings.ReplaceAll(p, `\`, `/`)
	p = strings.ReplaceAll(p, `:`, `\:`)
	p = strings.ReplaceAll(p, `'`, `'\''`)
	return p
}

func stitchHardCuts(transitions []*Transition, n int) bool {
	for i := 1; i < n; i++ {
		if i < len(transitions) && transitions[i] != nil && transitions[i].Duration > 0 {
			return false
		}
	}
	return true
}

func titleCardLavfi(seg Segment, bgColor, textColor string, w, h int, fps, dur float64) string {
	fpsN := fps
	if fpsN <= 0 {
		fpsN = 30
	}
	bgColor = ffmpegColor(bgColor)
	textColor = ffmpegColor(textColor)
	lavfi := fmt.Sprintf("color=c=%s:s=%dx%d:d=%.6f:r=%g", bgColor, w, h, dur, fpsN)
	kicker, main := splitChapterTitle(seg.Text)
	sub := strings.TrimSpace(seg.Subtitle)
	fontSize := seg.FontSize
	if fontSize <= 0 {
		fontSize = 96
	}
	if kicker != "" && fontSize < 80 {
		fontSize = 96
	}
	mainFont, regularFont := titleCardFontFiles(seg.Font)
	kickerSize := fontSize / 3
	if kickerSize < 22 {
		kickerSize = 22
	}
	subSize := fontSize / 3
	if subSize < 20 {
		subSize = 20
	}
	main = wrapTitleLine(main, 28)
	if kicker != "" {
		lavfi += "," + titleDrawtext(regularFont, strings.ToUpper(kicker), kickerSize, withAlpha(textColor, 0.55), "(w-text_w)/2", "(h-text_h)/2-"+fmt.Sprintf("%d", fontSize+kickerSize/2))
		lavfi += "," + titleDrawtext(mainFont, main, fontSize, textColor, "(w-text_w)/2", "(h-text_h)/2")
	} else if main != "" {
		shift := 0
		if sub != "" {
			shift = fontSize / 3
		}
		lavfi += "," + titleDrawtext(mainFont, main, fontSize, textColor, "(w-text_w)/2", fmt.Sprintf("(h-text_h)/2-%d", shift))
	}
	if sub != "" {
		lavfi += "," + titleDrawtext(regularFont, sub, subSize, withAlpha(textColor, 0.55), "(w-text_w)/2", "(h-text_h)/2+"+fmt.Sprintf("%d", fontSize/2+24))
	}
	return lavfi
}

func splitChapterTitle(text string) (kicker, main string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	if i := strings.Index(text, ": "); i >= 0 {
		return strings.TrimSpace(text[:i]), strings.TrimSpace(text[i+2:])
	}
	if i := strings.Index(text, ":"); i >= 0 {
		left, right := strings.TrimSpace(text[:i]), strings.TrimSpace(text[i+1:])
		if right != "" {
			return left, right
		}
	}
	return "", text
}

func wrapTitleLine(s string, width int) string {
	s = strings.TrimSpace(s)
	if width < 8 || len(s) <= width {
		return s
	}
	var lines []string
	for s != "" {
		if len(s) <= width {
			lines = append(lines, s)
			break
		}
		cut := strings.LastIndex(s[:width], " ")
		if cut <= 0 {
			cut = width
		}
		lines = append(lines, strings.TrimSpace(s[:cut]))
		s = strings.TrimSpace(s[cut:])
	}
	return strings.Join(lines, "\n")
}

func evenDim(n int) int {
	if n < 2 {
		return 2
	}
	return n - n%2
}

// FitExportSize returns even WxH that fit in maxW x maxH without upscaling.
func FitExportSize(w, h, maxW, maxH int) (int, int) {
	if maxW <= 0 {
		maxW = 1920
	}
	if maxH <= 0 {
		maxH = 1080
	}
	w, h = evenDim(w), evenDim(h)
	if w <= 0 || h <= 0 {
		return maxW, maxH
	}
	if w <= maxW && h <= maxH {
		return w, h
	}
	rw := float64(maxW) / float64(w)
	rh := float64(maxH) / float64(h)
	r := rw
	if rh < rw {
		r = rh
	}
	return evenDim(int(float64(w) * r)), evenDim(int(float64(h) * r))
}
