package video_api

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/sponsorblock"
	"thirdcoast.systems/rewind/internal/videoid"
)

// HandleMarkersRender returns an SSE-patched, server-rendered marker list.
// This replaces the former client-side MarkerManager.renderList() which built
// HTML via createElement.
func HandleMarkersRender(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		videoID := c.Param("id")

		videoRow, err := dbc.Queries(c.Request().Context()).GetVideoByID(c.Request().Context(), videoUUID)
		if err != nil || videoRow == nil {
			return nil
		}

		markers, err := dbc.Queries(c.Request().Context()).ListMarkersByVideo(c.Request().Context(), videoUUID)
		if err != nil {
			slog.Warn("markers render: failed to list", "error", err)
			markers = []*db.Marker{}
		}

		// Fetch SponsorBlock segments for YouTube videos
		sbMarkers := []*db.Marker{}
		if videoRow.Src != "" {
			if ytID, err := videoid.ExtractYouTubeVideoID(videoRow.Src); err == nil && ytID != "" {
				sb := sponsorblock.NewClient(os.Getenv("SPONSORBLOCK_BASE_URL"))
				ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
				defer cancel()

				segs, err := sb.GetSkipSegments(ctx, sponsorblock.SkipSegmentsParams{
					VideoID: ytID,
					Categories: []string{
						"sponsor", "intro", "outro", "selfpromo",
						"interaction", "music_offtopic", "preview", "chapter",
					},
					ActionTypes: []string{"skip", "mute", "chapter"},
				})
				if err != nil {
					slog.Warn("markers render: sponsorblock failed", "error", err)
				} else {
					for _, s := range segs {
						sbMarkers = append(sbMarkers, sponsorblock.SegmentToMarker(videoUUID, s))
					}
				}
			}
		}

		// Combine and sort by timestamp
		all := make([]*db.Marker, 0, len(markers)+len(sbMarkers))
		all = append(all, markers...)
		all = append(all, sbMarkers...)
		sort.Slice(all, func(i, j int) bool {
			return all[i].Timestamp < all[j].Timestamp
		})

		// Convert to templ-friendly items.
		// DB markers are indices 0..len(markers)-1, SB markers are len(markers)..len(all)-1.
		items := make([]components.MarkerItem, len(all))
		for i, m := range all {
			dur := 0.0
			if m.Duration != nil {
				dur = *m.Duration
			}
			items[i] = components.MarkerItem{
				ID:             m.ID.String(),
				Timestamp:      m.Timestamp,
				Duration:       dur,
				Title:          m.Title,
				Description:    m.Description,
				IsSponsorBlock: i >= len(markers),
			}
		}

		sse.PatchElementTempl(
			components.MarkerList(videoID, items),
			datastar.WithSelector("[data-markers-list]"),
			datastar.WithModeInner(),
		)

		return nil
	}
}
