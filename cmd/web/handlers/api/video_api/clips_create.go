// package video_api provides video-related API handlers.
package video_api

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/clip_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/filters"
)

// HandleClipsCreate creates a new clip for a video.
func HandleClipsCreate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		videoRow, err := dbc.Queries(ctx).GetVideoByID(ctx, videoUUID)
		if err != nil || videoRow == nil {
			return c.String(404, "video not found")
		}

		var req struct {
			StartTs     float64         `json:"start_ts"`
			EndTs       float64         `json:"end_ts"`
			Title       string          `json:"title"`
			Description string          `json:"description"`
			Color       string          `json:"color"`
			Tags        json.RawMessage `json:"tags"`
		}
		if err := c.Bind(&req); err != nil {
			return c.String(400, "invalid json")
		}

		if req.StartTs < 0 || req.EndTs < 0 {
			return c.String(400, "start_ts and end_ts must be >= 0")
		}
		if req.EndTs <= req.StartTs {
			return c.String(400, "end_ts must be > start_ts")
		}

		color := strings.TrimSpace(req.Color)
		if color == "" {
			color = randomClipColor()
		}

		tags := req.Tags
		if len(tags) == 0 {
			tags = json.RawMessage("[]")
		}
		var tmp any
		if err := json.Unmarshal(tags, &tmp); err != nil {
			return c.String(400, "invalid tags")
		}

		durFloat := req.EndTs - req.StartTs

		created, err := dbc.Queries(ctx).CreateClip(ctx, &db.CreateClipParams{
			VideoID:     videoUUID,
			StartTs:     req.StartTs,
			EndTs:       req.EndTs,
			Duration:    durFloat,
			Title:       req.Title,
			Description: req.Description,
			Color:       color,
			Tags:        tags,
			CreatedBy:   userUUID,
		})
		if err != nil {
			return c.String(500, "failed to create clip")
		}

		// SSE response for DataStar
		common.SetSSEHeaders(c)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		clips, err := dbc.Queries(ctx).ListClipsByVideo(ctx, videoUUID)
		if err != nil {
			clips = []*db.Clip{}
		}

		variant := "watch"
		referer := c.Request().Referer()
		if len(referer) > 0 && (referer[len(referer)-4:] == "/cut" || strings.Contains(referer, "/cut?")) {
			variant = "cut"
		}

		_ = sse.PatchElementTempl(
			components.ClipList(clips, variant),
			datastar.WithSelector("[data-clip-list]"),
			datastar.WithModeReplace(),
		)

		// Re-hydrate export status badges that were wiped by the full list replace.
		clip_api.PatchClipExportStatuses(sse, ctx, dbc, clips)

		// For the cut interface, directly patch the inspector form + signals
		// so the new clip is immediately selected and editable.
		// (The old approach used ExecuteScript to click the new row, but that
		// raced with DataStar's DOM processing and failed to trigger handlers.)
		if variant == "cut" {
			// Fetch the full clip for the inspector
			fullClip, err := dbc.Queries(ctx).GetClip(ctx, created.ID)
			if err == nil && fullClip != nil {
				// Patch the inspector form with the new clip's data
				_ = sse.PatchElementTempl(
					templates.ClipInspectorForm(fullClip),
					datastar.WithSelector("[data-cut-clip-form]"),
					datastar.WithModeReplace(),
				)

				// Patch signals to select the new clip
				newClipSignals, _ := json.Marshal(map[string]interface{}{
					"_filterStack":    []interface{}{},
					"_selectedClipId": fullClip.ID.String(),
					"_clipDirty":      false,
					"_clipStartTs":    fullClip.StartTs,
					"_clipEndTs":      fullClip.EndTs,
				})
				_ = sse.PatchSignals(newClipSignals)

				// Patch filter cards (empty for a new clip)
				_ = sse.PatchElementTempl(
					components.FilterCardList(nil, c.Param("id"), []filters.FilterOption{
						{Value: "", Label: "(select crop)"},
					}),
					datastar.WithSelectorID("filter-stack-list"),
				)

				// Patch export panel (no crops yet)
				_ = sse.PatchElementTempl(
					components.CutExportPanel(nil),
					datastar.WithSelectorID("cut-export-panel"),
				)
			}
		}

		return nil
	}
}

// VideosListParams holds validated query/signal parameters for video listing.
type VideosListParams struct {
	Query      string
	Sort       string
	Duration   string
	Uploader   string
	Tags       []string
	DateType   string // "archived" or "published"
	DateFrom   string // YYYY-MM-DD
	DateTo     string // YYYY-MM-DD
	HasClips   bool
	HasMarkers bool
	Page       int
	PageSize   int
}

// DefaultVideosListParams returns params with sensible defaults.
func DefaultVideosListParams() VideosListParams {
	return VideosListParams{
		Query:      "",
		Sort:       "newest",
		Duration:   "",
		Uploader:   "",
		Tags:       nil,
		DateType:   "archived",
		DateFrom:   "",
		DateTo:     "",
		HasClips:   false,
		HasMarkers: false,
		Page:       1,
		PageSize:   48,
	}
}

// Validate clamps and validates the parameters.
func (p *VideosListParams) Validate() {
	// Clamp page
	if p.Page < 1 {
		p.Page = 1
	}
	// Validate page size
	validSizes := map[int]bool{24: true, 48: true, 96: true}
	if !validSizes[p.PageSize] {
		p.PageSize = 48
	}
	// Validate sort
	validSorts := map[string]bool{
		"newest": true, "oldest": true,
		"published-newest": true, "published-oldest": true,
		"alpha": true, "alpha-desc": true,
		"duration": true, "duration-desc": true,
		"most-clips": true, "most-markers": true,
		"recently-clipped": true, "recently-marked": true,
	}
	if !validSorts[p.Sort] {
		p.Sort = "newest"
	}
	// Validate duration filter
	validDurations := map[string]bool{"": true, "short": true, "medium": true, "long": true}
	if !validDurations[p.Duration] {
		p.Duration = ""
	}
	// Validate date type
	if p.DateType != "published" {
		p.DateType = "archived"
	}
}

// Offset calculates the database offset.
func (p *VideosListParams) Offset() int32 {
	return int32((p.Page - 1) * p.PageSize)
}

// nullableString returns a pointer to s if non-empty, else nil.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullableBool returns a pointer to b if true, else nil.
func nullableBool(b bool) *bool {
	if !b {
		return nil
	}
	return &b
}

// parseDate parses a YYYY-MM-DD string into pgtype.Date
func parseDate(s string) pgtype.Date {
	if s == "" {
		return pgtype.Date{}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}

func parseTagsString(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// randomClipColor generates a vibrant random color in OKLCH space,
// converted to a hex string for storage. Uses L=65-75%, C=0.15-0.25,
// H=0-360 to ensure colors are saturated and visible on dark backgrounds.
func randomClipColor() string {
	L := 0.65 + rand.Float64()*0.10 // 65-75% lightness
	C := 0.15 + rand.Float64()*0.10 // moderate-high chroma
	H := rand.Float64() * 360.0     // full hue range

	r, g, b := oklchToSRGB(L, C, H)
	return fmt.Sprintf("#%02x%02x%02x", clampByte(r), clampByte(g), clampByte(b))
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 255
	}
	return uint8(math.Round(v * 255))
}

// oklchToSRGB converts OKLCH (L 0-1, C ≥0, H degrees) → linear sRGB → gamma sRGB.
func oklchToSRGB(L, C, H float64) (float64, float64, float64) {
	hRad := H * math.Pi / 180.0
	a := C * math.Cos(hRad)
	b := C * math.Sin(hRad)

	// OKLab → linear LMS
	l_ := L + 0.3963377774*a + 0.2158037573*b
	m_ := L - 0.1055613458*a - 0.0638541728*b
	s_ := L - 0.0894841775*a - 1.2914855480*b

	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	// Linear LMS → linear sRGB
	rLin := +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	gLin := -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	bLin := -0.0041960863*l - 0.7034186147*m + 1.7076147010*s

	return gammaEncode(rLin), gammaEncode(gLin), gammaEncode(bLin)
}

func gammaEncode(v float64) float64 {
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1.0/2.4) - 0.055
}
