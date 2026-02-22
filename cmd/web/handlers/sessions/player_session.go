package sessions

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandlePlayerSessionPage(dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		code := c.Param("code")
		if len(code) != 6 {
			return c.Redirect(302, "/player")
		}

		// Verify session exists
		_, err := dbc.Queries(c.Request().Context()).GetPlayerSessionByCode(c.Request().Context(), code)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.Redirect(302, "/player")
			}
			return c.String(500, "Session not found")
		}

		username := ""
		if u, ok := c.Request().Context().Value("username").(string); ok {
			username = u
		}
		return templates.RemotePlayer(code, username).Render(c.Request().Context(), c.Response())
	}
}

// defaultSceneJSON returns the default scene JSON bytes.
func defaultSceneJSON() []byte {
	return []byte(`{"version":1,"stage":{"aspect":"16:9"},"background":{"mode":"perlin-nebula","speed":1.0,"seed":0,"tint_oklch":{"l":1,"c":0,"h":0},"epoch_ms":0},"video":{"x":0.5,"y":0.5,"width":0.9,"height":0.9,"aspect":"","border":{"enabled":true,"size":2,"opacity":0.10}}}`)
}

// extractSceneFromState extracts the scene JSON from a session state blob.
func extractSceneFromState(state []byte) []byte {
	if len(state) == 0 {
		return defaultSceneJSON()
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(state, &m); err != nil {
		return defaultSceneJSON()
	}
	if raw, ok := m["scene"]; ok && len(raw) > 0 {
		return raw
	}
	return defaultSceneJSON()
}

// sceneToBase64 encodes scene JSON bytes to a base64 string.
func sceneToBase64(sceneJSON []byte) string {
	if len(sceneJSON) == 0 {
		sceneJSON = defaultSceneJSON()
	}
	return base64.StdEncoding.EncodeToString(sceneJSON)
}

// setSceneInState updates the scene field in the session state JSON.
func setSceneInState(state []byte, sceneJSON []byte) []byte {
	if len(sceneJSON) == 0 {
		sceneJSON = defaultSceneJSON()
	}

	var m map[string]json.RawMessage
	if len(state) > 0 {
		_ = json.Unmarshal(state, &m)
	}
	if m == nil {
		m = make(map[string]json.RawMessage)
	}
	m["scene"] = json.RawMessage(sceneJSON)

	out, err := json.Marshal(m)
	if err != nil {
		// Fall back to a minimal state.
		return []byte(`{"scene":` + string(sceneJSON) + `}`)
	}
	return out
}

// buildSceneFromForm extracts scene parameters from form values.
func buildSceneFromForm(c echo.Context) map[string]any {
	background := strings.TrimSpace(c.FormValue("background_mode"))
	if background == "" {
		background = "perlin-nebula"
	}

	speed := parseFloat(c.FormValue("background_speed"), 1.0, 0, 0)
	seed := parseFloat(c.FormValue("background_seed"), 0.0, 0, 0)
	l := parseFloat(c.FormValue("tint_l"), 1.0, 0, 1)
	cval := parseFloat(c.FormValue("tint_c"), 0.0, 0, 1)
	h := parseHue(c.FormValue("tint_h"), 0.0)

	videoX := parseFloat(c.FormValue("video_x"), 0.5, 0, 1)
	videoY := parseFloat(c.FormValue("video_y"), 0.5, 0, 1)
	videoW := parseFloat(c.FormValue("video_w"), 0.9, 0.1, 1)
	videoH := parseFloat(c.FormValue("video_h"), 0.9, 0.1, 1)

	stageAspect := strings.TrimSpace(c.FormValue("stage_aspect"))
	if stageAspect == "" {
		stageAspect = "16:9"
	}
	videoAspect := strings.TrimSpace(c.FormValue("video_aspect"))

	videoBorderEnabled := strings.TrimSpace(c.FormValue("video_border_enabled")) != ""
	videoBorderSize := parseFloat(c.FormValue("video_border_size"), 2.0, 0, 50)
	videoBorderOpacity := parseFloat(c.FormValue("video_border_opacity"), 0.10, 0, 1)

	return map[string]any{
		"version": 1,
		"stage": map[string]any{
			"aspect": stageAspect,
		},
		"background": map[string]any{
			"mode":  background,
			"speed": speed,
			"seed":  seed,
			"tint_oklch": map[string]any{
				"l": l,
				"c": cval,
				"h": h,
			},
		},
		"video": map[string]any{
			"x":      videoX,
			"y":      videoY,
			"width":  videoW,
			"height": videoH,
			"aspect": videoAspect,
			"border": map[string]any{
				"enabled": videoBorderEnabled,
				"size":    videoBorderSize,
				"opacity": videoBorderOpacity,
			},
		},
	}
}

// buildSceneFromFormWithEpoch builds a scene with epoch_ms timestamp.
func buildSceneFromFormWithEpoch(c echo.Context) map[string]any {
	scene := buildSceneFromForm(c)

	epochMs := time.Now().UnixMilli()
	if raw := strings.TrimSpace(c.FormValue("epoch_ms")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			epochMs = v
		}
	}

	bg := scene["background"].(map[string]any)
	bg["epoch_ms"] = epochMs

	return scene
}

// parseFloat parses a float from form value with default, min, max constraints.
func parseFloat(raw string, defaultVal, min, max float64) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return defaultVal
	}
	if max > 0 && v > max {
		return max
	}
	if v < min {
		return min
	}
	return v
}

// parseHue parses a hue value (wraps to 0-360 range).
func parseHue(raw string, defaultVal float64) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return defaultVal
	}
	for v < 0 {
		v += 360
	}
	for v >= 360 {
		v -= 360
	}
	return v
}
