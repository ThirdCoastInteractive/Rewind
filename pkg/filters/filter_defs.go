// Package filters defines the UI-facing filter parameter metadata and DataStar
// expression generators for the video editor's filter stack.
package filters

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// FilterParamType describes the kind of input control for a filter parameter.
type FilterParamType string

const (
	FilterParamRange  FilterParamType = "range"
	FilterParamSelect FilterParamType = "select"
	FilterParamNumber FilterParamType = "number"
	FilterParamText   FilterParamType = "text"
	FilterParamPreset FilterParamType = "preset"
	FilterParamColor  FilterParamType = "color"
	// FilterParamIconSelect renders options as a row of visual icon buttons
	// instead of a <select> dropdown. Each option should have an Icon field set.
	FilterParamIconSelect FilterParamType = "icon_select"
	// FilterParamPositionGrid renders a 3×3 clickable position grid.
	FilterParamPositionGrid FilterParamType = "position_grid"
	// FilterParamDial renders a circular SVG dial/knob for angular or continuous values.
	FilterParamDial FilterParamType = "dial"
	// FilterParamColorWheel renders an HSL color wheel with a draggable center dot.
	FilterParamColorWheel FilterParamType = "color_wheel"
)

// FilterOption is a single <option> in a select or preset dropdown.
type FilterOption struct {
	Value string
	Label string
	Icon  string // Font Awesome icon name (without fa- prefix), used by icon_select
}

// FilterParam describes one adjustable parameter for a filter type.
type FilterParam struct {
	Key         string
	Label       string
	Type        FilterParamType
	Min         float64
	Max         float64
	Step        float64
	DefaultVal  string // always serialised as a string for templ use
	Decimals    int
	Placeholder string
	Options     []FilterOption
	// Presets maps preset-value → flat map of param-key → value.
	// Used only when Type == FilterParamPreset.
	Presets map[string]map[string]string
	// TrackGradient is a CSS linear-gradient value for range slider tracks.
	// When set, the slider track displays this gradient instead of the default.
	TrackGradient string
	// HintMin and HintMax are semantic labels shown at the ends of a range slider
	// (e.g. "dark" / "bright" for brightness).
	HintMin string
	HintMax string
}

// FilterStackEntry mirrors the client-side {type, params} signal element
// used to persist the ordered filter stack in JSON.
type FilterStackEntry struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params"`
}

// ---------------------------------------------------------------------------
// Template helpers
// ---------------------------------------------------------------------------

// FmtNum formats a float for use in HTML attributes (no trailing zeros).
func FmtNum(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// ParamValue reads a parameter from the filter entry, returning defaultVal if
// missing. Works with string and float64 values from JSON decoding.
func ParamValue(params map[string]interface{}, key string, defaultVal string) string {
	if params == nil {
		return defaultVal
	}
	v, ok := params[key]
	if !ok || v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// FilterCardActionURL returns the SSE endpoint for re-rendering filter cards.
func FilterCardActionURL(videoID string) string {
	return fmt.Sprintf("/api/videos/%s/cut/filter-cards", videoID)
}

// FilterConfig bundles the SSE action URL and dirty-signal name used by filter
// expression functions, so the same filter UI components can be wired into
// different host pages with different endpoints and signals.
type FilterConfig struct {
	ActionURL   string // SSE endpoint that re-renders filter cards
	DirtySignal string // DataStar signal name to set true on changes (e.g. "_clipDirty")
}

// CutFilterConfig returns a FilterConfig for the cut-page filter stack.
func CutFilterConfig(videoID string) FilterConfig {
	return FilterConfig{
		ActionURL:   FilterCardActionURL(videoID),
		DirtySignal: "_clipDirty",
	}
}

// DefaultParamsJS returns a JS object literal string containing the default
// parameter values for the given filter type, e.g. "{value:0}" or
// "{angle:0.5}". Preset-type params expand their default preset's values.
func DefaultParamsJS(filterType string) string {
	params := ParamsForFilterType(filterType, nil)
	if len(params) == 0 {
		return "{}"
	}
	parts := []string{}
	for _, p := range params {
		if p.Type == FilterParamPreset && p.Presets != nil {
			// Expand the default preset's key→value pairs.
			if vals, ok := p.Presets[p.DefaultVal]; ok {
				for k, v := range vals {
					parts = append(parts, fmt.Sprintf("%s:'%s'", k, v))
				}
			}
		} else {
			parts = append(parts, fmt.Sprintf("%s:'%s'", p.Key, p.DefaultVal))
		}
	}
	result := "{"
	for i, part := range parts {
		if i > 0 {
			result += ","
		}
		result += part
	}
	result += "}"
	return result
}

// FilterAddExpr returns the DataStar expression for adding a filter.
func FilterAddExpr(filterType string, cfg FilterConfig) string {
	return fmt.Sprintf(
		"$_filterStack=[...$_filterStack.filter(f=>f&&typeof f==='object'),{type:'%s',params:%s}]; $%s=true; el.closest('details').open=false; @post('%s',{filterSignals:{include:/_filterStack|_selectedClipId/,exclude:/^$/}})",
		filterType, DefaultParamsJS(filterType), cfg.DirtySignal, cfg.ActionURL,
	)
}

// FilterRemoveExpr returns the DataStar expression for removing a filter.
func FilterRemoveExpr(index int, cfg FilterConfig) string {
	return fmt.Sprintf(
		"$_filterStack=$_filterStack.filter((f,i)=>i!==%d&&f&&typeof f==='object'); $%s=true; @post('%s',{filterSignals:{include:/_filterStack|_selectedClipId/,exclude:/^$/}})",
		index, cfg.DirtySignal, cfg.ActionURL,
	)
}

// FilterMoveExpr returns the expression for moving a filter up or down.
func FilterMoveExpr(index, direction int, cfg FilterConfig) string {
	newIdx := index + direction
	return fmt.Sprintf(
		"let s=[...$_filterStack.filter(f=>f&&typeof f==='object')]; [s[%d],s[%d]]=[s[%d],s[%d]]; $_filterStack=s; $%s=true; @post('%s',{filterSignals:{include:/_filterStack|_selectedClipId/,exclude:/^$/}})",
		index, newIdx, newIdx, index, cfg.DirtySignal, cfg.ActionURL,
	)
}

// FilterParamRangeExpr returns the expression for range slider input changes.
func FilterParamRangeExpr(index int, key string) string {
	return fmt.Sprintf(
		"let v=parseFloat(evt.target.value); let s=[...$_filterStack].filter(f=>f&&typeof f==='object'); s[%d]={...s[%d],params:{...(s[%d].params||{}),%s:v}}; $_filterStack=s; window.cutEditor?.filterPreview?.updateParam(%d,'%s',v)",
		index, index, index, key, index, key,
	)
}

// FilterParamDialUpdateExpr returns a JS snippet for the dial widget's
// onChange callback. It receives the new numeric value and writes it into
// the signal, mirroring FilterParamRangeExpr but without evt.target.
func FilterParamDialUpdateExpr(index int, key string) string {
	return fmt.Sprintf(
		"function(v){let s=[...$_filterStack].filter(f=>f&&typeof f==='object');s[%d]={...s[%d],params:{...(s[%d].params||{}),%s:v}};$_filterStack=s;window.cutEditor?.filterPreview?.updateParam(%d,'%s',v)}",
		index, index, index, key, index, key,
	)
}

// FilterParamSelectExpr returns the expression for select input changes.
func FilterParamSelectExpr(index int, key string) string {
	return fmt.Sprintf(
		"let s=[...$_filterStack].filter(f=>f&&typeof f==='object'); s[%d]={...s[%d],params:{...(s[%d].params||{}),%s:evt.target.value}}; $_filterStack=s; window.cutEditor?.filterPreview?.apply($_filterStack)",
		index, index, index, key,
	)
}

// FilterParamSetValueExpr returns a DataStar expression for setting a param to
// a literal value (used by icon_select and position_grid button clicks).
func FilterParamSetValueExpr(index int, key, value string, cfg FilterConfig) string {
	return fmt.Sprintf(
		"let s=[...$_filterStack].filter(f=>f&&typeof f==='object'); s[%d]={...s[%d],params:{...(s[%d].params||{}),%s:'%s'}}; $_filterStack=s; window.cutEditor?.filterPreview?.apply($_filterStack); $%s=true; @post('%s',{filterSignals:{include:/_filterStack|_selectedClipId/,exclude:/^$/}})",
		index, index, index, key, value, cfg.DirtySignal, cfg.ActionURL,
	)
}

// FilterParamNumberExpr returns the expression for number input changes.
func FilterParamNumberExpr(index int, key string) string {
	return fmt.Sprintf(
		"let v=parseFloat(evt.target.value); if(!isFinite(v))return; let s=[...$_filterStack].filter(f=>f&&typeof f==='object'); s[%d]={...s[%d],params:{...(s[%d].params||{}),%s:v}}; $_filterStack=s; window.cutEditor?.filterPreview?.updateParam(%d,'%s',v)",
		index, index, index, key, index, key,
	)
}

// FilterParamTextExpr returns the expression for text input changes.
func FilterParamTextExpr(index int, key string) string {
	return fmt.Sprintf(
		"let s=[...$_filterStack].filter(f=>f&&typeof f==='object'); s[%d]={...s[%d],params:{...(s[%d].params||{}),%s:evt.target.value}}; $_filterStack=s; window.cutEditor?.filterPreview?.apply($_filterStack)",
		index, index, index, key,
	)
}

// FilterParamColorExpr returns the expression for color picker input changes.
func FilterParamColorExpr(index int, key string) string {
	return fmt.Sprintf(
		"let s=[...$_filterStack].filter(f=>f&&typeof f==='object'); s[%d]={...s[%d],params:{...(s[%d].params||{}),%s:evt.target.value}}; $_filterStack=s; window.cutEditor?.filterPreview?.apply($_filterStack)",
		index, index, index, key,
	)
}

// FilterParamReadoutExpr returns a data-text expression for the range readout.
func FilterParamReadoutExpr(index int, key, defaultVal string, decimals int) string {
	return fmt.Sprintf(
		"%d<$_filterStack.length?(v=>(v===''||v==null)?%s:Number(v))($_filterStack[%d]?.params?.%s).toFixed(%d):%s",
		index, defaultVal, index, key, decimals, defaultVal,
	)
}

// FilterParamSyncExpr returns a data-effect expression that re-syncs an input's
// value from the signal. This is needed because the morph may set the input
// value before updating min/max attributes, causing the browser to clamp the
// value to the OLD range.
func FilterParamSyncExpr(index int, key, defaultVal string) string {
	return fmt.Sprintf(
		"if(%d<$_filterStack.length){let v=$_filterStack[%d]?.params?.%s;el.value=v??'%s'}",
		index, index, key, defaultVal,
	)
}

// FilterPresetExpr returns the expression for preset selection changes.
func FilterPresetExpr(index int) string {
	return fmt.Sprintf(
		"let p=JSON.parse(el.dataset.presets)[evt.target.value]; if(p){let s=[...$_filterStack].filter(f=>f&&typeof f==='object'); s[%d]={...s[%d],params:{...p,_preset:evt.target.value}}; $_filterStack=s; window.cutEditor?.filterPreview?.apply($_filterStack)}",
		index, index,
	)
}

// FilterParamSaveExpr returns a DataStar expression that marks dirty and
// re-renders filter cards via SSE. Does NOT persist to the database
// - filter_stack is saved only through the unified SAVE button.
func FilterParamSaveExpr(cfg FilterConfig) string {
	return fmt.Sprintf(
		"$%s=true; @post('%s',{filterSignals:{include:/_filterStack|_selectedClipId/,exclude:/^$/}})",
		cfg.DirtySignal, cfg.ActionURL,
	)
}

// FilterPresetDataAttr serialises a preset map to JSON for a data- attribute.
func FilterPresetDataAttr(presets map[string]map[string]string) string {
	b, _ := json.Marshal(presets)
	return string(b)
}

// IconForFilterType returns the Font-Awesome icon name for a filter type.
func IconForFilterType(t string) string {
	icons := map[string]string{
		"crop": "crop", "scale": "up-right-and-down-left-from-center", "transpose": "rotate-right",
		"rotate": "rotate", "hflip": "arrows-left-right", "vflip": "arrows-up-down", "pad": "border-all",
		"brightness": "sun", "contrast": "circle-half-stroke", "saturation": "palette",
		"gamma": "sliders", "color_balance": "swatchbook", "curves": "bezier-curve", "grayscale": "droplet-slash",
		"sepia": "image", "sharpen": "diamond", "denoise": "wand-magic-sparkles",
		"vignette": "bullseye", "color_temp": "temperature-half", "lift_gamma_gain": "sliders",
		"lut": "film", "exposure": "sun",
		"speed": "gauge-high", "fade_in": "right-long",
		"fade_out": "left-long", "reverse": "backward",
		"volume": "volume-high", "normalize": "chart-bar", "equalizer": "sliders", "bass": "speaker",
		"treble": "music", "compressor": "compress", "noise_gate": "volume-off", "highpass": "filter", "lowpass": "filter",
		"audio_fade_in": "volume-low", "audio_fade_out": "volume-xmark", "mute": "volume-xmark",
		"text": "font",
	}
	if v, ok := icons[t]; ok {
		return v
	}
	return "sliders"
}

// LabelForFilterType returns the human-readable label for a filter type.
func LabelForFilterType(t string) string {
	labels := map[string]string{
		"crop": "Crop", "scale": "Scale", "transpose": "Rotate 90°",
		"rotate": "Rotate", "hflip": "Flip H", "vflip": "Flip V", "pad": "Pad / Letterbox",
		"brightness": "Brightness", "contrast": "Contrast", "saturation": "Saturation",
		"gamma": "Gamma", "color_balance": "Color Balance", "curves": "Curves", "grayscale": "Grayscale",
		"sepia": "Sepia", "sharpen": "Sharpen", "denoise": "Denoise",
		"vignette": "Vignette", "color_temp": "Color Temperature", "lift_gamma_gain": "Lift / Gamma / Gain",
		"lut": "LUT Preset", "exposure": "Exposure",
		"speed": "Speed", "fade_in": "Fade In",
		"fade_out": "Fade Out", "reverse": "Reverse",
		"volume": "Volume", "normalize": "Normalize", "equalizer": "Equalizer", "bass": "Bass",
		"treble": "Treble", "compressor": "Compressor", "noise_gate": "Noise Gate", "highpass": "High Pass",
		"lowpass": "Low Pass", "audio_fade_in": "Audio Fade In",
		"audio_fade_out": "Audio Fade Out", "mute": "Mute Audio", "text": "Text",
	}
	if v, ok := labels[t]; ok {
		return v
	}
	return t
}

// CategoryForFilterType returns the CSS class suffix for the filter's category.
// Used for color-coded card borders.
func CategoryForFilterType(t string) string {
	switch t {
	case "crop", "scale", "transpose", "rotate", "hflip", "vflip", "pad":
		return "spatial"
	case "brightness", "contrast", "saturation", "gamma", "color_balance",
		"curves", "grayscale", "sepia", "sharpen", "denoise", "vignette",
		"color_temp", "lift_gamma_gain", "lut", "exposure":
		return "color"
	case "speed", "fade_in", "fade_out", "reverse":
		return "temporal"
	case "volume", "normalize", "equalizer", "bass", "treble", "compressor",
		"noise_gate", "highpass", "lowpass", "audio_fade_in", "audio_fade_out", "mute":
		return "audio"
	case "text":
		return "overlay"
	default:
		return "color"
	}
}

// ParamsForFilterType returns the parameter definitions for a given filter type.
// cropOptions is only used for the "crop" filter and may be nil.
func ParamsForFilterType(filterType string, cropOptions []FilterOption) []FilterParam {
	switch filterType {
	case "brightness":
		return []FilterParam{{Key: "value", Label: "Value", Type: FilterParamRange, Min: -1, Max: 1, Step: 0.01, DefaultVal: "0", Decimals: 2, TrackGradient: "linear-gradient(to right, #000, #888, #fff)", HintMin: "dark", HintMax: "bright"}}
	case "contrast":
		return []FilterParam{{Key: "value", Label: "Value", Type: FilterParamRange, Min: -2, Max: 2, Step: 0.01, DefaultVal: "1", Decimals: 2, TrackGradient: "linear-gradient(to right, #666, #888 40%, #000 50%, #fff 50%, #888 60%)", HintMin: "flat", HintMax: "punchy"}}
	case "saturation":
		return []FilterParam{{Key: "value", Label: "Value", Type: FilterParamRange, Min: 0, Max: 3, Step: 0.01, DefaultVal: "1", Decimals: 2, TrackGradient: "linear-gradient(to right, #888, #e44, #ea0, #0c0, #08f, #c4f)", HintMin: "gray", HintMax: "vivid"}}
	case "gamma":
		return []FilterParam{{Key: "value", Label: "Value", Type: FilterParamRange, Min: 0.5, Max: 3.0, Step: 0.05, DefaultVal: "1", Decimals: 2, TrackGradient: "linear-gradient(to right, #111, #333, #666, #aaa, #eee)", HintMin: "shadows", HintMax: "highlights"}}
	case "color_balance":
		return []FilterParam{{
			Key: "_preset", Label: "Style", Type: FilterParamPreset, DefaultVal: "warm",
			Presets: map[string]map[string]string{
				"warm":        {"rs": "0.1", "gs": "0", "bs": "-0.1", "rm": "0.1", "gm": "0.02", "bm": "-0.1", "rh": "0.05", "gh": "0", "bh": "-0.05"},
				"cool":        {"rs": "-0.1", "gs": "0", "bs": "0.1", "rm": "-0.1", "gm": "0", "bm": "0.1", "rh": "-0.05", "gh": "0", "bh": "0.05"},
				"sunset":      {"rs": "0.2", "gs": "0.1", "bs": "-0.1", "rm": "0.15", "gm": "0.05", "bm": "-0.15", "rh": "0", "gh": "0", "bh": "0"},
				"moonlight":   {"rs": "-0.05", "gs": "-0.02", "bs": "0.15", "rm": "-0.05", "gm": "0", "bm": "0.1", "rh": "0", "gh": "0", "bh": "0.05"},
				"teal_orange": {"rs": "0.15", "gs": "-0.05", "bs": "-0.15", "rm": "0", "gm": "0", "bm": "0", "rh": "-0.1", "gh": "0.05", "bh": "0.1"},
			},
			Options: []FilterOption{
				{Value: "warm", Label: "Warm"},
				{Value: "cool", Label: "Cool"},
				{Value: "sunset", Label: "Sunset"},
				{Value: "moonlight", Label: "Moonlight"},
				{Value: "teal_orange", Label: "Teal & Orange"},
			},
		}}
	case "sharpen":
		return []FilterParam{{Key: "amount", Label: "Amount", Type: FilterParamRange, Min: 0, Max: 5, Step: 0.1, DefaultVal: "1.5", Decimals: 1, HintMin: "soft", HintMax: "sharp"}}
	case "vignette":
		return []FilterParam{{Key: "angle", Label: "Amount", Type: FilterParamRange, Min: 0, Max: 1, Step: 0.01, DefaultVal: "0.5", Decimals: 2, HintMin: "none", HintMax: "heavy"}}
	case "rotate":
		return []FilterParam{{Key: "angle", Label: "Angle", Type: FilterParamDial, Min: -180, Max: 180, Step: 0.5, DefaultVal: "0", Decimals: 1}}
	case "speed":
		return []FilterParam{{Key: "factor", Label: "Factor", Type: FilterParamDial, Min: 0.25, Max: 4, Step: 0.05, DefaultVal: "1", Decimals: 2, HintMin: "slow", HintMax: "fast"}}
	case "fade_in":
		return []FilterParam{
			{Key: "duration", Label: "Duration", Type: FilterParamRange, Min: 0.1, Max: 10, Step: 0.1, DefaultVal: "0.5", Decimals: 1},
			{Key: "offset", Label: "Start At", Type: FilterParamNumber, Min: 0, Max: 300, Step: 0.1, DefaultVal: "0", Placeholder: "secs from start"},
			{Key: "color", Label: "Color", Type: FilterParamColor, DefaultVal: "#000000"},
		}
	case "fade_out":
		return []FilterParam{
			{Key: "duration", Label: "Duration", Type: FilterParamRange, Min: 0.1, Max: 10, Step: 0.1, DefaultVal: "0.5", Decimals: 1},
			{Key: "offset", Label: "Before End", Type: FilterParamNumber, Min: 0, Max: 300, Step: 0.1, DefaultVal: "0", Placeholder: "secs from end"},
			{Key: "color", Label: "Color", Type: FilterParamColor, DefaultVal: "#000000"},
		}
	case "audio_fade_in":
		return []FilterParam{
			{Key: "duration", Label: "Duration", Type: FilterParamNumber, Min: 0.1, Max: 30, Step: 0.1, DefaultVal: "0.5"},
			{Key: "offset", Label: "Start At", Type: FilterParamNumber, Min: 0, Max: 300, Step: 0.1, DefaultVal: "0", Placeholder: "secs from start"},
			{Key: "curve", Label: "Curve", Type: FilterParamSelect, DefaultVal: "tri",
				Options: []FilterOption{
					{Value: "tri", Label: "Linear"},
					{Value: "qsin", Label: "Quarter Sine"},
					{Value: "esin", Label: "Exp Sine"},
					{Value: "log", Label: "Logarithmic"},
					{Value: "par", Label: "Parabola"},
					{Value: "exp", Label: "Exponential"},
				},
			},
		}
	case "audio_fade_out":
		return []FilterParam{
			{Key: "duration", Label: "Duration", Type: FilterParamNumber, Min: 0.1, Max: 30, Step: 0.1, DefaultVal: "0.5"},
			{Key: "offset", Label: "Before End", Type: FilterParamNumber, Min: 0, Max: 300, Step: 0.1, DefaultVal: "0", Placeholder: "secs from end"},
			{Key: "curve", Label: "Curve", Type: FilterParamSelect, DefaultVal: "tri",
				Options: []FilterOption{
					{Value: "tri", Label: "Linear"},
					{Value: "qsin", Label: "Quarter Sine"},
					{Value: "esin", Label: "Exp Sine"},
					{Value: "log", Label: "Logarithmic"},
					{Value: "par", Label: "Parabola"},
					{Value: "exp", Label: "Exponential"},
				},
			},
		}
	case "volume":
		return []FilterParam{{Key: "gain", Label: "Gain", Type: FilterParamRange, Min: 0, Max: 3, Step: 0.01, DefaultVal: "1", Decimals: 2, TrackGradient: "linear-gradient(to right, #333, #22c55e 33%, #eab308 66%, #ef4444)", HintMin: "mute", HintMax: "boost"}}
	case "bass", "treble":
		return []FilterParam{{Key: "gain", Label: "dB", Type: FilterParamRange, Min: -12, Max: 12, Step: 0.5, DefaultVal: "0", Decimals: 1, HintMin: "cut", HintMax: "boost"}}
	case "highpass":
		return []FilterParam{{Key: "frequency", Label: "Hz", Type: FilterParamNumber, Min: 20, Max: 20000, Step: 10, DefaultVal: "200"}}
	case "lowpass":
		return []FilterParam{{Key: "frequency", Label: "Hz", Type: FilterParamNumber, Min: 20, Max: 20000, Step: 10, DefaultVal: "3000"}}
	case "equalizer":
		return []FilterParam{{
			Key: "_preset", Label: "Style", Type: FilterParamPreset, DefaultVal: "voice_clarity",
			Presets: map[string]map[string]string{
				"bass_boost":    {"frequency": "100", "width": "200", "gain": "6"},
				"treble_boost":  {"frequency": "8000", "width": "4000", "gain": "6"},
				"voice_clarity": {"frequency": "3000", "width": "2000", "gain": "4"},
				"de_mud":        {"frequency": "300", "width": "200", "gain": "-4"},
				"air":           {"frequency": "12000", "width": "4000", "gain": "3"},
				"sub_cut":       {"frequency": "60", "width": "60", "gain": "-12"},
			},
			Options: []FilterOption{
				{Value: "voice_clarity", Label: "Voice Clarity"},
				{Value: "bass_boost", Label: "Bass Boost"},
				{Value: "treble_boost", Label: "Treble Boost"},
				{Value: "de_mud", Label: "De-Mud"},
				{Value: "air", Label: "Air"},
				{Value: "sub_cut", Label: "Sub Cut"},
			},
		}}
	case "scale":
		return []FilterParam{{Key: "width", Label: "Width", Type: FilterParamNumber, Min: 128, Max: 7680, Step: 2, DefaultVal: "1920"}}
	case "pad":
		return []FilterParam{
			{Key: "width", Label: "Width", Type: FilterParamNumber, Min: 128, Max: 7680, Step: 2, DefaultVal: "1920"},
			{Key: "height", Label: "Height", Type: FilterParamNumber, Min: 128, Max: 4320, Step: 2, DefaultVal: "1080"},
			{Key: "color", Label: "Color", Type: FilterParamColor, DefaultVal: "#000000"},
		}
	case "curves":
		return []FilterParam{{
			Key: "preset", Label: "Preset", Type: FilterParamSelect, DefaultVal: "vintage",
			Options: []FilterOption{
				{Value: "vintage", Label: "Vintage"},
				{Value: "cross_process", Label: "Cross Process"},
				{Value: "lighter", Label: "Lighter"},
				{Value: "darker", Label: "Darker"},
				{Value: "increase_contrast", Label: "Increase Contrast"},
				{Value: "negative", Label: "Negative"},
			},
		}}
	case "transpose":
		return []FilterParam{{
			Key: "direction", Label: "Dir", Type: FilterParamIconSelect, DefaultVal: "cw",
			Options: []FilterOption{
				{Value: "cw", Label: "CW 90°", Icon: "rotate-right"},
				{Value: "ccw", Label: "CCW 90°", Icon: "rotate-left"},
				{Value: "ccw_flip", Label: "CCW+Flip", Icon: "arrows-rotate"},
				{Value: "cw_flip", Label: "CW+Flip", Icon: "arrows-spin"},
			},
		}}
	case "denoise":
		return []FilterParam{{
			Key: "strength", Label: "Level", Type: FilterParamIconSelect, DefaultVal: "medium",
			Options: []FilterOption{
				{Value: "light", Label: "Light", Icon: "feather"},
				{Value: "medium", Label: "Medium", Icon: "wand-magic-sparkles"},
				{Value: "heavy", Label: "Heavy", Icon: "shield-halved"},
			},
		}}
	case "normalize":
		return []FilterParam{{
			Key: "mode", Label: "Mode", Type: FilterParamIconSelect, DefaultVal: "loudnorm",
			Options: []FilterOption{
				{Value: "peak", Label: "Peak", Icon: "mountain"},
				{Value: "rms", Label: "RMS", Icon: "wave-square"},
				{Value: "loudnorm", Label: "Loudnorm", Icon: "chart-bar"},
			},
		}}
	case "compressor":
		return []FilterParam{{
			Key: "_preset", Label: "Style", Type: FilterParamPreset, DefaultVal: "medium",
			Presets: map[string]map[string]string{
				"light":     {"threshold": "-16", "ratio": "2", "attack": "20", "release": "250"},
				"medium":    {"threshold": "-20", "ratio": "4", "attack": "20", "release": "250"},
				"heavy":     {"threshold": "-30", "ratio": "8", "attack": "5", "release": "500"},
				"broadcast": {"threshold": "-24", "ratio": "6", "attack": "10", "release": "300"},
			},
			Options: []FilterOption{
				{Value: "light", Label: "Light"},
				{Value: "medium", Label: "Medium"},
				{Value: "heavy", Label: "Heavy"},
				{Value: "broadcast", Label: "Broadcast"},
			},
		}}
	case "noise_gate":
		return []FilterParam{{Key: "threshold", Label: "Thresh dB", Type: FilterParamRange, Min: -60, Max: 0, Step: 1, DefaultVal: "-40", Decimals: 0, HintMin: "silent", HintMax: "open"}}
	case "text":
		return []FilterParam{
			{Key: "text", Label: "Text", Type: FilterParamText, DefaultVal: "", Placeholder: "Watermark text"},
			{Key: "position", Label: "Pos", Type: FilterParamPositionGrid, DefaultVal: "bottom-right",
				Options: []FilterOption{
					{Value: "top-left", Label: "Top Left"},
					{Value: "top-center", Label: "Top Center"},
					{Value: "top-right", Label: "Top Right"},
					{Value: "center", Label: "Center"},
					{Value: "bottom-left", Label: "Bottom Left"},
					{Value: "bottom-center", Label: "Bottom Center"},
					{Value: "bottom-right", Label: "Bottom Right"},
				},
			},
			{Key: "font_size", Label: "Size", Type: FilterParamRange, Min: 8, Max: 200, Step: 1, DefaultVal: "24", Decimals: 0},
			{Key: "color", Label: "Color", Type: FilterParamColor, DefaultVal: "#ffffff"},
		}
	case "crop":
		opts := cropOptions
		if opts == nil {
			opts = []FilterOption{{Value: "", Label: "(select crop)"}}
		}
		return []FilterParam{{
			Key: "crop_id", Label: "Crop", Type: FilterParamSelect, DefaultVal: "",
			Options: opts,
		}}

	// === New Resolve-inspired filters ===

	case "color_temp":
		return []FilterParam{
			{Key: "temperature", Label: "Temp", Type: FilterParamRange, Min: 1000, Max: 12000, Step: 100, DefaultVal: "6500", Decimals: 0, TrackGradient: "linear-gradient(to right, #ff8a00, #ffd4a0, #fff, #a8c8ff, #4080ff)", HintMin: "warm", HintMax: "cool"},
			{Key: "tint", Label: "Tint", Type: FilterParamRange, Min: -1, Max: 1, Step: 0.01, DefaultVal: "0", Decimals: 2, TrackGradient: "linear-gradient(to right, #00ff88, #fff, #ff00ff)", HintMin: "green", HintMax: "magenta"},
		}

	case "lift_gamma_gain":
		return []FilterParam{
			{Key: "lift", Label: "Lift", Type: FilterParamRange, Min: -0.5, Max: 0.5, Step: 0.01, DefaultVal: "0", Decimals: 2, TrackGradient: "linear-gradient(to right, #000, #555, #aaa)", HintMin: "darken", HintMax: "lighten"},
			{Key: "gamma", Label: "Gamma", Type: FilterParamRange, Min: 0.5, Max: 2.0, Step: 0.01, DefaultVal: "1", Decimals: 2, TrackGradient: "linear-gradient(to right, #222, #666, #ccc)", HintMin: "darken", HintMax: "lighten"},
			{Key: "gain", Label: "Gain", Type: FilterParamRange, Min: 0.5, Max: 2.0, Step: 0.01, DefaultVal: "1", Decimals: 2, TrackGradient: "linear-gradient(to right, #444, #999, #fff)", HintMin: "darken", HintMax: "lighten"},
		}

	case "exposure":
		return []FilterParam{
			{Key: "exposure", Label: "EV", Type: FilterParamRange, Min: -3, Max: 3, Step: 0.05, DefaultVal: "0", Decimals: 2, TrackGradient: "linear-gradient(to right, #111, #444, #888, #bbb, #fff)", HintMin: "-3 EV", HintMax: "+3 EV"},
			{Key: "black", Label: "Black Pt", Type: FilterParamRange, Min: 0, Max: 0.1, Step: 0.001, DefaultVal: "0", Decimals: 3, TrackGradient: "linear-gradient(to right, #000, #222)", HintMin: "deep", HintMax: "crushed"},
		}

	case "lut":
		return []FilterParam{{
			Key: "preset", Label: "LUT", Type: FilterParamIconSelect, DefaultVal: "none",
			Options: []FilterOption{
				{Value: "none", Label: "None", Icon: "ban"},
				{Value: "cinematic_warm", Label: "Cine Warm", Icon: "sun"},
				{Value: "cinematic_cool", Label: "Cine Cool", Icon: "snowflake"},
				{Value: "film_noir", Label: "Film Noir", Icon: "moon"},
				{Value: "bleach_bypass", Label: "Bleach", Icon: "droplet"},
				{Value: "orange_teal", Label: "O&T", Icon: "palette"},
				{Value: "vintage_fade", Label: "Vintage", Icon: "camera-retro"},
				{Value: "high_contrast", Label: "Hi-Con", Icon: "circle-half-stroke"},
				{Value: "pastel", Label: "Pastel", Icon: "cloud"},
				{Value: "golden_hour", Label: "Golden", Icon: "sun"},
				{Value: "moonlit", Label: "Moonlit", Icon: "moon"},
			},
		}}

	default:
		// hflip, vflip, grayscale, sepia, reverse, mute - no params
		return nil
	}
}
