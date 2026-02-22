// package video_api provides video-related API handlers.
package video_api

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleSeekSheet(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}
		level := strings.TrimSpace(c.Param("level"))
		sheet := strings.TrimSpace(c.Param("sheet"))
		if !ReSeekLevelParam.MatchString(level) {
			return c.String(400, "invalid level")
		}
		if !ReSeekSheetParam.MatchString(sheet) {
			return c.String(400, "invalid sheet")
		}
		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		dir, err := fileserver.GetVideoDirForID(c.Request().Context(), videoUUID.String())
		if err != nil {
			return err
		}
		path := filepath.Join(dir, "seek", "levels", level, sheet)
		if _, err := os.Stat(path); err != nil {
			return c.String(404, "seek thumbnail sheet not available")
		}
		return fs.ServeDiskFileWithCache(c, path, "image/jpeg", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagWeakStat)
	}
}
