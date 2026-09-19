// Package scene holds the composited-scene JSON contract shared by the producer
// (which edits and broadcasts a scene) and the viewer/program output (which
// renders it). In producer v2 the active scene lives on show_notes.scene_state.
// The JSON shape is the v1 contract; Phase 4 extends it for the Three.js compositor.
package scene

import (
	"encoding/base64"
	"encoding/json"
	"math"
)

// DefaultJSON returns the default scene (Perlin-nebula background, centered video frame).
func DefaultJSON() []byte {
	return []byte(`{"version":1,"stage":{"aspect":"16:9"},"background":{"mode":"perlin-nebula","speed":1.0,"seed":0,"tint_oklch":{"l":1,"c":0,"h":0},"epoch_ms":0},"video":{"x":0.5,"y":0.5,"width":0.9,"height":0.9,"aspect":"","border":{"enabled":true,"size":2,"opacity":0.10}}}`)
}

// FromState returns the stored scene JSON, or the default if absent/invalid.
func FromState(state []byte) []byte {
	if len(state) == 0 || string(state) == "{}" || !json.Valid(state) {
		return DefaultJSON()
	}
	return state
}

// ToBase64 encodes scene JSON for embedding in the player-scene element.
func ToBase64(sceneJSON []byte) string {
	if len(sceneJSON) == 0 {
		sceneJSON = DefaultJSON()
	}
	return base64.StdEncoding.EncodeToString(sceneJSON)
}

// Params are the scene fields editable from the producer UI.
type Params struct {
	BackgroundMode string  `json:"background_mode"`
	Speed          float64 `json:"background_speed"`
	Seed           float64 `json:"background_seed"`
	TintL          float64 `json:"tint_l"`
	TintC          float64 `json:"tint_c"`
	TintH          float64 `json:"tint_h"`
	StageAspect    string  `json:"stage_aspect"`
	VideoX         float64 `json:"video_x"`
	VideoY         float64 `json:"video_y"`
	VideoW         float64 `json:"video_w"`
	VideoH         float64 `json:"video_h"`
	VideoAspect    string  `json:"video_aspect"`
	BorderEnabled  bool    `json:"video_border_enabled"`
	BorderSize     float64 `json:"video_border_size"`
	BorderOpacity  float64 `json:"video_border_opacity"`
	// Layout controls how cameras + content are arranged by the compositor:
	// content-pip | content-only | grid | solo | duo.
	Layout string `json:"layout"`
	// Content is the "now playing" program video on the content mesh, with
	// transport controls. Sync model: the active position at wall-clock `now` is
	// `t + (now - t0_epoch_ms)/1000` while playing, or fixed at `t` while paused.
	ContentVideoID string  `json:"content_video_id"`
	ContentSrc     string  `json:"content_src"`
	ContentT0      int64   `json:"content_t0"` // epoch ms of the anchor
	ContentT       float64 `json:"content_t"`  // anchor playback position (seconds)
	ContentPaused  bool    `json:"content_paused"`
	ContentVolume  float64 `json:"content_volume"`
	ContentMuted   bool    `json:"content_muted"`
	ContentLoop    bool    `json:"content_loop"`
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func wrapHue(v float64) float64 {
	v = math.Mod(v, 360)
	if v < 0 {
		v += 360
	}
	return v
}

// Restamp updates a stored scene's epoch_ms to "now" so its animation re-syncs
// across all clients when re-applied. Returns the input unchanged on error.
func Restamp(sceneJSON []byte, epochMs int64) []byte {
	var m map[string]any
	if err := json.Unmarshal(sceneJSON, &m); err != nil {
		return sceneJSON
	}
	bg, ok := m["background"].(map[string]any)
	if !ok {
		bg = map[string]any{}
		m["background"] = bg
	}
	bg["epoch_ms"] = epochMs
	out, err := json.Marshal(m)
	if err != nil {
		return sceneJSON
	}
	return out
}

// Build assembles a scene JSON document from producer params, stamping epoch_ms
// so all clients animate in sync.
func Build(p Params, epochMs int64) []byte {
	if p.BackgroundMode == "" {
		p.BackgroundMode = "perlin-nebula"
	}
	if p.StageAspect == "" {
		p.StageAspect = "16:9"
	}
	if p.Layout == "" {
		p.Layout = "content-pip"
	}
	// Default unset (zero) video dimensions so a partial apply still centers a
	// reasonably-sized content frame rather than collapsing to the clamp minimum.
	vx, vy, vw, vh := p.VideoX, p.VideoY, p.VideoW, p.VideoH
	if vx == 0 {
		vx = 0.5
	}
	if vy == 0 {
		vy = 0.5
	}
	if vw == 0 {
		vw = 0.9
	}
	if vh == 0 {
		vh = 0.9
	}
	doc := map[string]any{
		"version": 1,
		"layout":  p.Layout,
		"stage":   map[string]any{"aspect": p.StageAspect},
		"background": map[string]any{
			"mode":       p.BackgroundMode,
			"speed":      clamp(p.Speed, 0, 5),
			"seed":       p.Seed,
			"tint_oklch": map[string]any{"l": clamp(p.TintL, 0, 1), "c": clamp(p.TintC, 0, 1), "h": wrapHue(p.TintH)},
			"epoch_ms":   epochMs,
		},
		"video": map[string]any{
			"x":      clamp(vx, 0, 1),
			"y":      clamp(vy, 0, 1),
			"width":  clamp(vw, 0.1, 1),
			"height": clamp(vh, 0.1, 1),
			"aspect": p.VideoAspect,
			"border": map[string]any{
				"enabled": p.BorderEnabled,
				"size":    clamp(p.BorderSize, 0, 50),
				"opacity": clamp(p.BorderOpacity, 0, 1),
			},
		},
	}
	if p.ContentVideoID != "" && p.ContentSrc != "" {
		vol := p.ContentVolume
		if vol <= 0 {
			vol = 1 // unset → full; use muted for silence
		}
		doc["content"] = map[string]any{
			"video_id":    p.ContentVideoID,
			"src":         p.ContentSrc,
			"t0_epoch_ms": p.ContentT0,
			"t":           p.ContentT,
			"paused":      p.ContentPaused,
			"volume":      clamp(vol, 0, 1),
			"muted":       p.ContentMuted,
			"loop":        p.ContentLoop,
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return DefaultJSON()
	}
	return out
}
