// package clip_api provides clip-related API handlers.
package clip_api

import (
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleSeek(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		clipUUID, err := common.RequireUUIDParam(c, "clipId")
		if err != nil {
			return err
		}

		clip, err := dbc.Queries(ctx).GetClip(ctx, clipUUID)
		if err != nil || clip == nil {
			return c.String(404, "clip not found")
		}

		// SSE headers
		common.SetSSEHeaders(c)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		_ = sse.ExecuteScript(fmt.Sprintf(`
			{
				const video = document.getElementById('videoPlayer');
				if (video) {
					video.currentTime = %f;
				}
			}
		`, clip.StartTs))

		return nil
	}
}

// HandleCropCreate creates a new crop for a clip.
