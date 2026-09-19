package stitch_api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

type youtubeRequest struct {
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

func editorYouTube(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, _, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return err
		}
		var req youtubeRequest
		if err := editorJSON(c, &req); err != nil {
			return err
		}
		if req.Tags == nil {
			req.Tags = []string{}
		}
		snap, err := stitch.NewStore(dbc).SetYouTube(c.Request().Context(), owner, project, req.Description, req.Tags)
		if err != nil {
			return editorError(c, err)
		}
		return c.JSON(http.StatusOK, snapshotJSON(snap))
	}
}
