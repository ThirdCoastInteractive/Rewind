package content

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleVideoCutPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		videoRow, err := dbc.Queries(c.Request().Context()).GetVideoByID(c.Request().Context(), videoUUID)
		if err != nil || videoRow == nil {
			return c.String(404, "video not found")
		}

		videoData, err := dbc.Queries(c.Request().Context()).GetVideoWithDownloadJob(c.Request().Context(), videoUUID)
		if err != nil {
			slog.Error("failed to fetch video", "error", err)
			return c.String(404, "video not found")
		}

		video := templates.VideoDetail{
			ID:        videoData.VideoID.String(),
			Src:       videoData.Src,
			Title:     videoData.Title,
			Info:      videoData.Info,
			SpoolDir:  "",
			CreatedAt: videoData.VideoCreatedAt.Time.Format("January 2, 2006 at 3:04 PM"),
		}

		if videoData.SpoolDir != nil {
			video.SpoolDir = *videoData.SpoolDir
		}

		clips, err := dbc.Queries(c.Request().Context()).ListClipsByVideo(c.Request().Context(), videoUUID)
		if err != nil {
			slog.Warn("failed to fetch clips for cut page", "error", err, "video_id", videoUUID)
			clips = []*db.Clip{}
		}

		keybindings := map[string]string{}
		if rows, err := dbc.Queries(c.Request().Context()).GetUserKeybindings(c.Request().Context(), userUUID); err == nil {
			keybindings = common.KeybindingsRowsToMap(rows)
		}

		return templates.VideoCutPage(video, clips, username, keybindings).Render(c.Request().Context(), c.Response())
	}
}
