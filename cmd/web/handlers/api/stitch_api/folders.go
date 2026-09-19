package stitch_api

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func stitchReturnURL(c echo.Context) string {
	if next := strings.TrimSpace(c.FormValue("next")); strings.HasPrefix(next, "/stitch") {
		return next
	}
	if ref := c.Request().Header.Get("Referer"); strings.Contains(ref, "/stitch") {
		return ref
	}
	return "/stitch"
}

// HandleCreateFolder creates a folder owned by the current user.
func HandleCreateFolder(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(http.StatusFound, "/login")
		}
		name := strings.TrimSpace(c.FormValue("name"))
		if name == "" || len(name) > 120 {
			return c.String(http.StatusBadRequest, "folder name required")
		}
		var parent pgtype.UUID
		if raw := strings.TrimSpace(c.FormValue("parent_id")); raw != "" {
			parent, err = common.ParseUUID(raw)
			if err != nil {
				return c.String(http.StatusBadRequest, "invalid parent")
			}
			folder, lookupErr := dbc.Queries(c.Request().Context()).GetStitchFolder(c.Request().Context(), parent)
			if lookupErr != nil || folder.CreatedBy != userUUID {
				return c.String(http.StatusBadRequest, "invalid parent")
			}
		}
		_, err = dbc.Queries(c.Request().Context()).CreateStitchFolder(c.Request().Context(), &db.CreateStitchFolderParams{
			CreatedBy: userUUID,
			ParentID:  parent,
			Name:      name,
		})
		if err != nil {
			return c.String(http.StatusConflict, "could not create folder")
		}
		return c.Redirect(http.StatusFound, stitchReturnURL(c))
	}
}

// HandleDeleteFolder removes a folder owned by the current user. Projects become unfiled.
func HandleDeleteFolder(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(http.StatusFound, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		if err := dbc.Queries(c.Request().Context()).DeleteStitchFolder(c.Request().Context(), &db.DeleteStitchFolderParams{
			ID:        id,
			CreatedBy: userUUID,
		}); err != nil {
			return c.String(http.StatusInternalServerError, "could not delete folder")
		}
		return c.Redirect(http.StatusFound, "/stitch")
	}
}

// HandleMoveProject files a project into a folder owned by the current user.
func HandleMoveProject(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(http.StatusFound, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		var folderID pgtype.UUID
		if raw := strings.TrimSpace(c.FormValue("folder_id")); raw != "" {
			folderID, err = common.ParseUUID(raw)
			if err != nil {
				return c.String(http.StatusBadRequest, "invalid folder")
			}
			folder, lookupErr := dbc.Queries(c.Request().Context()).GetStitchFolder(c.Request().Context(), folderID)
			if lookupErr != nil || folder.CreatedBy != userUUID {
				return c.String(http.StatusBadRequest, "invalid folder")
			}
		}
		if err := dbc.Queries(c.Request().Context()).MoveStitchProject(c.Request().Context(), &db.MoveStitchProjectParams{
			FolderID:  folderID,
			ID:        id,
			CreatedBy: userUUID,
		}); err != nil {
			return c.String(http.StatusInternalServerError, "could not move project")
		}
		return c.Redirect(http.StatusFound, stitchReturnURL(c))
	}
}
