package clip_api

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"net/http"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func requireClipMutate(c echo.Context, sm *auth.SessionManager, q *db.Queries, clip *db.Clip, user pgtype.UUID) error {
	if clip == nil {
		return echo.NewHTTPError(http.StatusNotFound, "clip not found")
	}
	if _, err := common.RequireVideo(c, q, clip.VideoID, plugin.ActionVideoWrite); err != nil {
		return err
	}
	if clip.CreatedBy == user || sm.GetAccessLevel(c.Request()) == auth.AccessAdmin {
		return nil
	}
	if !common.LiveWorkspaceWrite() {
		return echo.NewHTTPError(http.StatusForbidden, "forbidden")
	}
	return nil
}

func requireClipRead(c echo.Context, sm *auth.SessionManager, q *db.Queries, clip *db.Clip, user pgtype.UUID) error {
	if clip == nil {
		return echo.NewHTTPError(http.StatusNotFound, "clip not found")
	}
	if _, err := common.RequireVideo(c, q, clip.VideoID, plugin.ActionVideoRead); err != nil {
		return err
	}
	if clip.CreatedBy == user || sm.GetAccessLevel(c.Request()) == auth.AccessAdmin || common.LiveWorkspaceWrite() {
		return nil
	}
	return echo.NewHTTPError(http.StatusForbidden, "forbidden")
}
