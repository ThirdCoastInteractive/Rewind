package ffmpeg

import (
	"fmt"
	"math"

	"thirdcoast.systems/rewind/pkg/utils/crops"
)

// ExportSpec describes the full encoding recipe for a clip export.
type ExportSpec struct {
	// Format is the output container format: "mp4", "webm", "mkv"
	Format string `json:"format,omitempty"`
	// Quality selects the encoding quality tier: "high" (CRF 21), "max" (CRF 18)
	Quality string `json:"quality,omitempty"`
	// Filters is an ordered list of filters to apply (video + audio).
	Filters []FilterSpec `json:"filters,omitempty"`
}

// FilterSpec describes a single filter in the export pipeline.
type FilterSpec struct {
	// Type identifies the filter: "crop", "scale", "brightness", "contrast", etc.
	Type string `json:"type"`
	// Params holds type-specific parameters as a loosely-typed map.
	Params map[string]any `json:"params,omitempty"`
}

// CompileFilters converts a slice of FilterSpec into ffmpeg Options.
// clipCrops is needed to resolve crop IDs to coordinates.
func CompileFilters(specs []FilterSpec, clipCrops crops.CropArray) ([]Option, error) {
	var opts []Option
	hasCrop := false

	for i, spec := range specs {
		filterOpts, err := compileFilter(spec, clipCrops)
		if err != nil {
			return nil, fmt.Errorf("filter[%d] (%s): %w", i, spec.Type, err)
		}
		opts = append(opts, filterOpts...)

		if spec.Type == "crop" || spec.Type == "crop_manual" {
			hasCrop = true
		}
	}

	// If any crop was applied, ensure even dimensions for h264 compatibility
	if hasCrop {
		opts = append(opts, EvenDimensions())
	}

	return opts, nil
}

// compileFilter converts a single FilterSpec into one or more ffmpeg Options.
func compileFilter(spec FilterSpec, clipCrops crops.CropArray) ([]Option, error) {
	switch spec.Type {

	// === Video - Spatial ===

	case "crop":
		cropID, _ := spec.Params["crop_id"].(string)
		if cropID == "" {
			return nil, fmt.Errorf("crop_id is required")
		}
		filter := crops.BuildCropFilterByID(clipCrops, cropID)
		if filter == "" {
			return nil, nil // Full frame or not found - skip
		}
		return []Option{Filter(filter)}, nil

	case "crop_manual":
		x := paramFloat(spec.Params, "x", 0.5)
		y := paramFloat(spec.Params, "y", 0.5)
		w := paramFloat(spec.Params, "width", 1.0)
		h := paramFloat(spec.Params, "height", 1.0)
		filter := crops.FFmpegCropFilter(x, y, w, h)
		if filter == "" {
			return nil, nil
		}
		return []Option{Filter(filter)}, nil

	case "scale":
		width := paramInt(spec.Params, "width", -2)
		height := paramInt(spec.Params, "height", -2)
		if width == -2 && height == -2 {
			return nil, fmt.Errorf("at least one of width or height is required")
		}
		return []Option{Scale(width, height)}, nil

	case "transpose":
		dir, _ := spec.Params["direction"].(string)
		switch dir {
		case "cw":
			return []Option{Filter("transpose=1")}, nil
		case "ccw":
			return []Option{Filter("transpose=2")}, nil
		case "cw_flip":
			return []Option{Filter("transpose=3")}, nil
		case "ccw_flip":
			return []Option{Filter("transpose=0")}, nil
		default:
			return []Option{Filter("transpose=1")}, nil // Default: CW
		}

	case "hflip":
		return []Option{Filter("hflip")}, nil

	case "vflip":
		return []Option{Filter("vflip")}, nil

	case "rotate":
		angle := paramFloat(spec.Params, "angle", 0)
		if angle == 0 {
			return nil, nil
		}
		return []Option{Filter(fmt.Sprintf("rotate=%f*PI/180", angle))}, nil

	case "pad":
		w := paramInt(spec.Params, "width", 0)
		h := paramInt(spec.Params, "height", 0)
		color := paramColor(spec.Params, "color", "black")
		if w <= 0 || h <= 0 {
			return nil, fmt.Errorf("width and height are required for pad")
		}
		return []Option{Filter(fmt.Sprintf("pad=%d:%d:(ow-iw)/2:(oh-ih)/2:%s", w, h, color))}, nil

	// === Video - Temporal ===

	case "speed":
		factor := paramFloat(spec.Params, "factor", 1.0)
		if factor == 1.0 {
			return nil, nil
		}
		if factor <= 0 || factor > 4.0 {
			return nil, fmt.Errorf("speed factor must be between 0.25 and 4.0")
		}
		opts := []Option{Filter(fmt.Sprintf("setpts=PTS/%.4f", factor))}
		// Audio atempo only supports 0.5-2.0 range; chain for larger ranges
		opts = append(opts, atempoChain(factor)...)
		return opts, nil

	case "fade_in":
		dur := paramFloat(spec.Params, "duration", 0.5)
		offset := paramFloat(spec.Params, "offset", 0)
		color := paramColor(spec.Params, "color", "black")
		filter := fmt.Sprintf("fade=t=in:st=%.3f:d=%.3f", offset, dur)
		if color != "black" && color != "#000000" {
			filter += fmt.Sprintf(":c=%s", color)
		}
		return []Option{Filter(filter)}, nil

	case "fade_out":
		dur := paramFloat(spec.Params, "duration", 0.5)
		color := paramColor(spec.Params, "color", "black")
		// Note: start time for fade_out must be calculated by the encoder
		// from clip duration. offset param (seconds before clip end) is stored
		// in the spec but applied by the encoder when it knows total duration.
		filter := fmt.Sprintf("fade=t=out:d=%.3f", dur)
		if color != "black" && color != "#000000" {
			filter += fmt.Sprintf(":c=%s", color)
		}
		return []Option{Filter(filter)}, nil

	case "reverse":
		return []Option{Filter("reverse"), AudioFilter("areverse")}, nil

	// === Video - Color & Effects ===

	case "brightness":
		v := paramFloat(spec.Params, "value", 0)
		if v == 0 {
			return nil, nil
		}
		return []Option{Filter(fmt.Sprintf("eq=brightness=%.4f", v))}, nil

	case "contrast":
		v := paramFloat(spec.Params, "value", 1.0)
		if v == 1.0 {
			return nil, nil
		}
		return []Option{Filter(fmt.Sprintf("eq=contrast=%.4f", v))}, nil

	case "saturation":
		v := paramFloat(spec.Params, "value", 1.0)
		if v == 1.0 {
			return nil, nil
		}
		return []Option{Filter(fmt.Sprintf("eq=saturation=%.4f", v))}, nil

	case "gamma":
		v := paramFloat(spec.Params, "value", 1.0)
		if v == 1.0 {
			return nil, nil
		}
		return []Option{Filter(fmt.Sprintf("eq=gamma=%.4f", v))}, nil

	case "curves":
		preset, _ := spec.Params["preset"].(string)
		if preset == "" {
			return nil, fmt.Errorf("preset is required for curves filter")
		}
		return []Option{Filter(fmt.Sprintf("curves=preset=%s", preset))}, nil

	case "grayscale":
		return []Option{Filter("hue=s=0")}, nil

	case "sepia":
		return []Option{Filter("colorchannelmixer=.393:.769:.189:0:.349:.686:.168:0:.272:.534:.131")}, nil

	case "sharpen":
		amount := paramFloat(spec.Params, "amount", 1.5)
		return []Option{Filter(fmt.Sprintf("unsharp=5:5:%.2f:5:5:0", amount))}, nil

	case "denoise":
		strength, _ := spec.Params["strength"].(string)
		switch strength {
		case "heavy":
			return []Option{Filter("hqdn3d=8:6:12:9")}, nil
		case "medium":
			return []Option{Filter("hqdn3d=4:3:6:4.5")}, nil
		default: // "light" or default
			return []Option{Filter("hqdn3d=2:1.5:3:2.25")}, nil
		}

	case "vignette":
		angle := paramFloat(spec.Params, "angle", 0.628) // PI/5 default
		return []Option{Filter(fmt.Sprintf("vignette=a=%.4f", angle))}, nil

	case "color_balance":
		parts := ""
		for _, key := range []string{"rs", "gs", "bs", "rm", "gm", "bm", "rh", "gh", "bh"} {
			if v, ok := spec.Params[key]; ok {
				if parts != "" {
					parts += ":"
				}
				parts += fmt.Sprintf("%s=%v", key, v)
			}
		}
		if parts == "" {
			return nil, nil
		}
		return []Option{Filter(fmt.Sprintf("colorbalance=%s", parts))}, nil

	case "color_temp":
		// Color temperature via colortemperature filter (FFmpeg 5.1+)
		// Falls back to colorbalance approximation.
		temp := paramFloat(spec.Params, "temperature", 6500)
		tint := paramFloat(spec.Params, "tint", 0)
		if temp == 6500 && tint == 0 {
			return nil, nil
		}
		var opts []Option
		if temp != 6500 {
			opts = append(opts, Filter(fmt.Sprintf("colortemperature=temperature=%.0f", temp)))
		}
		if tint != 0 {
			// Tint shifts green-magenta via colorbalance
			gShift := tint * 0.2
			mShift := -tint * 0.2
			opts = append(opts, Filter(fmt.Sprintf("colorbalance=gm=%.3f:bm=%.3f", gShift, mShift)))
		}
		return opts, nil

	case "lift_gamma_gain":
		// Lift/Gamma/Gain - maps to eq filter with curves
		lift := paramFloat(spec.Params, "lift", 0)
		gamma := paramFloat(spec.Params, "gamma", 1)
		gain := paramFloat(spec.Params, "gain", 1)
		if lift == 0 && gamma == 1 && gain == 1 {
			return nil, nil
		}
		// Combine into eq filter: brightness for lift, gamma for gamma, contrast for gain
		filter := fmt.Sprintf("eq=brightness=%.4f:gamma=%.4f:contrast=%.4f", lift, gamma, gain)
		return []Option{Filter(filter)}, nil

	case "exposure":
		ev := paramFloat(spec.Params, "exposure", 0)
		black := paramFloat(spec.Params, "black", 0)
		if ev == 0 && black == 0 {
			return nil, nil
		}
		var opts []Option
		if ev != 0 {
			// Exposure via curves multiplication: 2^EV
			// Use eq brightness approximation
			opts = append(opts, Filter(fmt.Sprintf("eq=brightness=%.4f", ev*0.15)))
		}
		if black > 0 {
			// Black point via curves
			opts = append(opts, Filter(fmt.Sprintf("curves=m='0/%.3f 1/1'", black)))
		}
		return opts, nil

	case "lut":
		preset, _ := spec.Params["preset"].(string)
		if preset == "" || preset == "none" {
			return nil, nil
		}
		return compileLUTPreset(preset)

	// === Video - Overlay & Text ===

	case "text":
		text, _ := spec.Params["text"].(string)
		if text == "" {
			return nil, nil
		}
		fontSize := paramInt(spec.Params, "font_size", 24)
		color := paramColor(spec.Params, "color", "white")
		position, _ := spec.Params["position"].(string)
		x, y := textPosition(position)
		return []Option{Filter(fmt.Sprintf("drawtext=text='%s':fontsize=%d:fontcolor=%s:x=%s:y=%s", text, fontSize, color, x, y))}, nil

	// === Audio ===

	case "volume":
		gain := paramFloat(spec.Params, "gain", 1.0)
		if gain == 1.0 {
			return nil, nil
		}
		return []Option{AudioFilter(fmt.Sprintf("volume=%.4f", gain))}, nil

	case "audio_fade_in":
		dur := paramFloat(spec.Params, "duration", 0.5)
		offset := paramFloat(spec.Params, "offset", 0)
		curve, _ := spec.Params["curve"].(string)
		filter := fmt.Sprintf("afade=t=in:st=%.3f:d=%.3f", offset, dur)
		if curve != "" && curve != "tri" {
			filter += fmt.Sprintf(":curve=%s", curve)
		}
		return []Option{AudioFilter(filter)}, nil

	case "audio_fade_out":
		dur := paramFloat(spec.Params, "duration", 0.5)
		curve, _ := spec.Params["curve"].(string)
		// Start time calculated by encoder from clip duration
		filter := fmt.Sprintf("afade=t=out:d=%.3f", dur)
		if curve != "" && curve != "tri" {
			filter += fmt.Sprintf(":curve=%s", curve)
		}
		return []Option{AudioFilter(filter)}, nil

	case "normalize":
		mode, _ := spec.Params["mode"].(string)
		switch mode {
		case "rms":
			return []Option{AudioFilter("dynaudnorm")}, nil
		case "peak":
			return []Option{AudioFilter("dynaudnorm=p=1")}, nil
		default: // "loudnorm" default
			return []Option{AudioFilter("loudnorm")}, nil
		}

	case "equalizer":
		freq := paramFloat(spec.Params, "frequency", 1000)
		width := paramFloat(spec.Params, "width", 200)
		gain := paramFloat(spec.Params, "gain", 0)
		if gain == 0 {
			return nil, nil
		}
		return []Option{AudioFilter(fmt.Sprintf("equalizer=f=%.0f:width_type=h:w=%.0f:g=%.1f", freq, width, gain))}, nil

	case "bass":
		gain := paramFloat(spec.Params, "gain", 0)
		if gain == 0 {
			return nil, nil
		}
		return []Option{AudioFilter(fmt.Sprintf("equalizer=f=100:width_type=h:w=200:g=%.1f", gain))}, nil

	case "treble":
		gain := paramFloat(spec.Params, "gain", 0)
		if gain == 0 {
			return nil, nil
		}
		return []Option{AudioFilter(fmt.Sprintf("equalizer=f=8000:width_type=h:w=4000:g=%.1f", gain))}, nil

	case "highpass":
		freq := paramInt(spec.Params, "frequency", 80)
		return []Option{AudioFilter(fmt.Sprintf("highpass=f=%d", freq))}, nil

	case "lowpass":
		freq := paramInt(spec.Params, "frequency", 15000)
		return []Option{AudioFilter(fmt.Sprintf("lowpass=f=%d", freq))}, nil

	case "compressor":
		thresholdDB := paramFloat(spec.Params, "threshold", -20)
		ratio := paramFloat(spec.Params, "ratio", 4)
		attack := paramFloat(spec.Params, "attack", 20)
		release := paramFloat(spec.Params, "release", 250)
		// ffmpeg acompressor expects threshold as linear 0.000976563–1, not dB
		threshold := math.Pow(10, thresholdDB/20.0)
		threshold = math.Max(0.000976563, math.Min(1.0, threshold))
		return []Option{AudioFilter(fmt.Sprintf("acompressor=threshold=%.6f:ratio=%.1f:attack=%.0f:release=%.0f", threshold, ratio, attack, release))}, nil

	case "noise_gate":
		thresholdDB := paramFloat(spec.Params, "threshold", -40)
		// ffmpeg agate expects threshold as linear 0–1, not dB
		threshold := math.Pow(10, thresholdDB/20.0)
		threshold = math.Max(0.0, math.Min(1.0, threshold))
		return []Option{AudioFilter(fmt.Sprintf("agate=threshold=%.6f", threshold))}, nil

	case "mute":
		return []Option{NoAudio}, nil

	default:
		return nil, fmt.Errorf("unknown filter type: %s", spec.Type)
	}
}

// atempoChain builds a chain of atempo filters for speed changes.
// atempo only supports 0.5-2.0 range, so we chain multiple for larger values.
func atempoChain(factor float64) []Option {
	if factor <= 0 {
		return nil
	}
	var opts []Option
	remaining := factor
	for remaining > 2.0 {
		opts = append(opts, AudioFilter("atempo=2.0"))
		remaining /= 2.0
	}
	for remaining < 0.5 {
		opts = append(opts, AudioFilter("atempo=0.5"))
		remaining /= 0.5
	}
	if remaining != 1.0 {
		opts = append(opts, AudioFilter(fmt.Sprintf("atempo=%.4f", remaining)))
	}
	return opts
}

// compileLUTPreset converts a named LUT preset into FFmpeg filter chains.
// These use combinations of curves, colorbalance, and eq to emulate common
// film look LUTs without requiring external .cube files.
func compileLUTPreset(preset string) ([]Option, error) {
	switch preset {
	case "cinematic_warm":
		return []Option{
			Filter("curves=preset=cross_process"),
			Filter("colorbalance=rs=0.08:gs=0.02:bs=-0.06:rm=0.05:gm=0.02:bm=-0.05:rh=0.03:gh=0:bh=-0.03"),
			Filter("eq=contrast=1.1:saturation=0.9"),
		}, nil
	case "cinematic_cool":
		return []Option{
			Filter("colorbalance=rs=-0.05:gs=0:bs=0.1:rm=-0.05:gm=0.02:bm=0.08:rh=-0.03:gh=0:bh=0.05"),
			Filter("eq=contrast=1.15:saturation=0.85"),
		}, nil
	case "film_noir":
		return []Option{
			Filter("hue=s=0"),
			Filter("eq=contrast=1.4:brightness=0.05:gamma=0.9"),
			Filter("curves=m='0/0 0.25/0.15 0.5/0.5 0.75/0.85 1/1'"),
		}, nil
	case "bleach_bypass":
		return []Option{
			Filter("eq=saturation=0.4:contrast=1.3:brightness=0.05"),
			Filter("curves=m='0/0 0.25/0.2 0.75/0.85 1/1'"),
		}, nil
	case "orange_teal":
		return []Option{
			Filter("colorbalance=rs=0.15:gs=-0.05:bs=-0.15:rm=0.05:gm=0:bm=-0.05:rh=-0.1:gh=0.05:bh=0.1"),
			Filter("eq=saturation=1.2:contrast=1.1"),
		}, nil
	case "vintage_fade":
		return []Option{
			Filter("curves=m='0/0.05 0.25/0.18 0.75/0.82 1/0.95'"),
			Filter("colorbalance=rs=0.1:gs=0.05:bs=-0.05:rm=0.05:gm=0:bm=-0.03"),
			Filter("eq=saturation=0.7"),
		}, nil
	case "high_contrast":
		return []Option{
			Filter("hue=s=0"),
			Filter("eq=contrast=1.6:brightness=-0.02"),
		}, nil
	case "pastel":
		return []Option{
			Filter("eq=saturation=0.6:brightness=0.08:gamma=1.1"),
			Filter("curves=m='0/0.05 0.5/0.55 1/0.95'"),
		}, nil
	case "golden_hour":
		return []Option{
			Filter("colorbalance=rs=0.15:gs=0.08:bs=-0.1:rm=0.1:gm=0.05:bm=-0.08"),
			Filter("eq=saturation=1.15:brightness=0.03:gamma=1.05"),
		}, nil
	case "moonlit":
		return []Option{
			Filter("colorbalance=rs=-0.08:gs=-0.02:bs=0.15:rm=-0.05:gm=0:bm=0.1:rh=0:gh=0:bh=0.05"),
			Filter("eq=saturation=0.6:brightness=-0.05:gamma=0.9"),
		}, nil
	default:
		return nil, fmt.Errorf("unknown LUT preset: %s", preset)
	}
}

// textPosition maps a named position to ffmpeg drawtext x/y expressions.
func textPosition(position string) (string, string) {
	switch position {
	case "top-left":
		return "10", "10"
	case "top-center":
		return "(w-text_w)/2", "10"
	case "top-right":
		return "w-text_w-10", "10"
	case "center":
		return "(w-text_w)/2", "(h-text_h)/2"
	case "bottom-left":
		return "10", "h-text_h-10"
	case "bottom-center":
		return "(w-text_w)/2", "h-text_h-10"
	case "bottom-right":
		return "w-text_w-10", "h-text_h-10"
	default:
		return "(w-text_w)/2", "h-text_h-10" // Default: bottom-center
	}
}

// paramColor extracts a color string from a params map with a default value.
// Handles both CSS color names ("black") and hex colors ("#000000").
// FFmpeg accepts both formats in filter params.
func paramColor(params map[string]any, key string, def string) string {
	v, ok := params[key]
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return def
	}
	return s
}

// paramFloat extracts a float64 from a params map with a default value.
func paramFloat(params map[string]any, key string, def float64) float64 {
	v, ok := params[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		var f float64
		if _, err := fmt.Sscanf(n, "%f", &f); err == nil {
			return f
		}
	}
	return def
}

// paramInt extracts an int from a params map with a default value.
func paramInt(params map[string]any, key string, def int) int {
	v, ok := params[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case float32:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		var i int
		if _, err := fmt.Sscanf(n, "%d", &i); err == nil {
			return i
		}
	}
	return def
}
