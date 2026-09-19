package video_api

import (
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

type uploaderOption struct {
	Name       string `json:"name"`
	VideoCount int64  `json:"videoCount"`
}

// HandleUploaderOptions lazily supplies the native uploader combobox. Keeping
// this out of the page render makes the library shell independent of catalog size.
func HandleUploaderOptions(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.JSON(401, map[string]string{"error": "unauthorized"})
		}
		rows, err := dbc.Queries(c.Request().Context()).SearchUploaders(c.Request().Context(), strings.TrimSpace(c.QueryParam("q")))
		if err != nil {
			return err
		}
		options := make([]uploaderOption, 0, len(rows))
		for _, row := range rows {
			options = append(options, uploaderOption{Name: row.Uploader, VideoCount: row.VideoCount})
		}
		return c.JSON(200, options)
	}
}
