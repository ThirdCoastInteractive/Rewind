// package video_api provides video-related API handlers.
package video_api

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/sponsorblock"
	"thirdcoast.systems/rewind/internal/videoid"
)

// HandleMarkers returns markers for a video including SponsorBlock segments.
func HandleMarkers(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		videoRow, err := dbc.Queries(c.Request().Context()).GetVideoByID(c.Request().Context(), videoUUID)
		if err != nil || videoRow == nil {
			return c.String(404, "video not found")
		}

		markers, err := dbc.Queries(c.Request().Context()).ListMarkersByVideo(c.Request().Context(), videoUUID)
		if err != nil {
			return c.String(500, "failed to list markers")
		}

		// Start with DB markers
		baseMarkers := markers
		sbMarkers := []*db.Marker{}

		// Add sponsorblock segments if this is a YouTube video
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
					slog.Warn("sponsorblock: failed to load segments", "error", err, "videoID", ytID)
				} else {
					for _, s := range segs {
						sbMarkers = append(sbMarkers, sponsorblock.SegmentToMarker(videoUUID, s))
					}
				}
			}
		}

		// Combine both marker sources
		all := make([]*db.Marker, 0, len(baseMarkers)+len(sbMarkers))
		all = append(all, baseMarkers...)
		all = append(all, sbMarkers...)

		sort.Slice(all, func(i, j int) bool {
			if all[i].Timestamp == all[j].Timestamp {
				return all[i].ID.String() < all[j].ID.String()
			}
			return all[i].Timestamp < all[j].Timestamp
		})

		// Convert to JSON-friendly format
		type MarkerResponse struct {
			ID          string        `json:"id"`
			VideoID     string        `json:"video_id"`
			Timestamp   float64       `json:"timestamp"`
			Duration    *float64      `json:"duration,omitempty"`
			Title       string        `json:"title"`
			Description string        `json:"description"`
			Color       string        `json:"color"`
			MarkerType  db.MarkerType `json:"marker_type"`
		}

		response := make([]MarkerResponse, len(all))
		for i, m := range all {
			response[i] = MarkerResponse{
				ID:          m.ID.String(),
				VideoID:     m.VideoID.String(),
				Timestamp:   m.Timestamp,
				Duration:    m.Duration,
				Title:       m.Title,
				Description: m.Description,
				Color:       m.Color,
				MarkerType:  m.MarkerType,
			}
		}

		return c.JSON(200, response)
	}
}
