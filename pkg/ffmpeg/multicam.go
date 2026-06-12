package ffmpeg

import (
	"fmt"
	"math"
	"strings"
	"time"

	"thirdcoast.systems/rewind/pkg/utils/crops"
)

// MultiCropCommand builds a single-input ffmpeg command that switches between
// different crop regions with xfade transitions. Uses split→trim→crop→xfade
// with a single decode pass. Audio is trimmed continuously (no crossfade needed
// since it's the same source).
func MultiCropCommand(
	input string,
	shots crops.ShotList,
	cropMap map[string]crops.Crop,
	outputWidth, outputHeight int,
	output string,
	opts ...Option,
) *Command {
	const outputFPS float64 = 30

	if outputWidth <= 0 || outputHeight <= 0 {
		outputWidth, outputHeight = InferMulticamDimensions(shots, cropMap, 0, 0)
	}

	n := len(shots)
	args := []string{"-hide_banner", "-y"}

	// Single input — seek to the earliest shot start for efficiency
	earliest := shots[0].Start
	latest := shots[n-1].End
	totalSourceDur := latest - earliest

	args = append(args,
		"-ss", formatDuration(time.Duration(earliest*float64(time.Second))),
		"-t", formatDuration(time.Duration(totalSourceDur*float64(time.Second))),
		"-i", input,
	)

	// All shot timestamps are now relative to the -ss seek point
	var chains []string

	// Normalization applied after crop: scale UP to fill output, pad any rounding remainder, fps, format.
	// Uses force_original_aspect_ratio=increase so the cropped region fills the frame (upscale).
	// lanczos gives sharper upscaling than the default bilinear.
	postCropNorm := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase:flags=lanczos,crop=%d:%d,fps=%d,format=yuv420p,setsar=1",
		outputWidth, outputHeight, outputWidth, outputHeight, int(outputFPS))

	// Pre-compute quantized durations for xfade offset accuracy
	qDurs := make([]float64, n)
	for i, shot := range shots {
		raw := shot.End - shot.Start
		frames := math.Floor(raw * outputFPS)
		qDurs[i] = frames / outputFPS
	}

	// Split the video stream into N copies
	splitLabels := make([]string, n)
	for i := range shots {
		splitLabels[i] = fmt.Sprintf("[s%d]", i)
	}
	chains = append(chains,
		fmt.Sprintf("[0:v]split=%d%s", n, strings.Join(splitLabels, "")),
	)

	// Per-shot: trim → crop → normalize to output dimensions
	for i, shot := range shots {
		relStart := shot.Start - earliest
		relEnd := shot.End - earliest

		cr := cropMap[shot.CropID]
		cropFilter := crops.FFmpegCropFilter(cr.X, cr.Y, cr.Width, cr.Height)

		trimChain := fmt.Sprintf("trim=start=%.6f:end=%.6f,setpts=PTS-STARTPTS", relStart, relEnd)
		if cropFilter != "" {
			trimChain += "," + cropFilter
		}
		trimChain += "," + postCropNorm

		chains = append(chains,
			fmt.Sprintf("[s%d]%s[v%d]", i, trimChain, i),
		)
	}

	// Audio: single continuous trim from the source (no crossfade needed)
	totalOutputDur := 0.0
	for i := range shots {
		totalOutputDur += qDurs[i]
	}
	// Subtract transition overlaps
	for i := 1; i < n; i++ {
		prevShot := shots[i-1]
		if prevShot.TransitionOut != nil && prevShot.TransitionOut.Duration > 0 {
			totalOutputDur -= prevShot.TransitionOut.Duration
		} else {
			totalOutputDur -= 2.0 / outputFPS // hard cut: 2-frame overlap
		}
	}

	audioNorm := "aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo"
	chains = append(chains,
		fmt.Sprintf("[0:a]atrim=0:%.6f,asetpts=PTS-STARTPTS,%s[audio]", totalOutputDur, audioNorm),
	)

	// Pairwise xfade chain (video only — audio is continuous)
	prevV := "v0"
	accDur := qDurs[0]

	for i := 1; i < n; i++ {
		nextV := fmt.Sprintf("v%d", i)
		outV := fmt.Sprintf("xv%d", i)

		prevShot := shots[i-1]
		var trType string
		var trDurSec float64
		if prevShot.TransitionOut != nil && prevShot.TransitionOut.Duration > 0 {
			trType = prevShot.TransitionOut.Type
			trDurSec = prevShot.TransitionOut.Duration
		} else {
			trType = "fade"
			trDurSec = 2.0 / outputFPS
		}

		offset := accDur - trDurSec

		chains = append(chains,
			fmt.Sprintf("[%s][%s]xfade=transition=%s:duration=%.6f:offset=%.6f[%s]",
				prevV, nextV, trType, trDurSec, offset, outV),
		)

		accDur = accDur + qDurs[i] - trDurSec
		prevV = outV
	}

	filterComplex := strings.Join(chains, ";\n    ")
	args = append(args, "-filter_complex", filterComplex)

	// Map final video and audio streams
	args = append(args, "-map", "["+prevV+"]", "-map", "[audio]")

	// Apply codec/quality options
	scratch := &Command{}
	for _, opt := range opts {
		opt.Apply(scratch)
	}
	args = append(args, scratch.postInput...)

	if strings.HasSuffix(strings.ToLower(output), ".mp4") ||
		strings.HasSuffix(strings.ToLower(output), ".mov") {
		args = append(args, "-movflags", "+faststart")
	}

	args = append(args, output)
	return &Command{rawArgs: args}
}

// InferMulticamDimensions picks output dimensions based on the crop aspect
// ratios and an optional target long-edge resolution. sourceAspect is the
// source video's width/height ratio (e.g., 16.0/9.0 for a 2560x1440 source),
// needed because crop Width/Height are normalized 0-1 fractions of the source
// frame, not absolute pixel dimensions. targetLongEdge of 0 defaults to 1920.
func InferMulticamDimensions(shots crops.ShotList, cropMap map[string]crops.Crop, sourceAspect float64, targetLongEdge int) (int, int) {
	if targetLongEdge <= 0 {
		targetLongEdge = 1920
	}
	if sourceAspect <= 0 {
		sourceAspect = 16.0 / 9.0
	}
	if len(shots) == 0 {
		return targetLongEdge, targetLongEdge * 9 / 16
	}
	cr, ok := cropMap[shots[0].CropID]
	if !ok || cr.Width <= 0 || cr.Height <= 0 {
		return targetLongEdge, targetLongEdge * 9 / 16
	}

	// Convert normalized crop dimensions to actual pixel aspect ratio.
	// crop.Width/Height are fractions of source frame, so multiply by
	// source dimensions to get pixel dimensions.
	cropPixelAspect := (cr.Width / cr.Height) * sourceAspect

	if cropPixelAspect >= 1.0 {
		w := targetLongEdge
		h := int(math.Round(float64(w)/cropPixelAspect/2.0)) * 2
		return w, h
	}
	h := targetLongEdge
	w := int(math.Round(float64(h)*cropPixelAspect/2.0)) * 2
	return w, h
}
