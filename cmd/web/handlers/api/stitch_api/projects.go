package stitch_api

import (
	"encoding/json"
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

// HandleCreateProject creates a new stitch project and redirects to its editor.
func HandleCreateProject(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		ctx := c.Request().Context()
		title := "Untitled"
		doc, err := json.Marshal(stitch.Document{
			Version:  stitch.CurrentVersion,
			Title:    title,
			FPS:      30,
			Width:    1920,
			Height:   1080,
			Segments: []stitch.Segment{},
			Settings: stitch.Settings{Format: "mp4", Quality: "high"},
		})
		if err != nil {
			slog.Error("failed to build stitch document", "error", err)
			return c.String(500, "failed to create project")
		}
		projectID, err := dbc.Queries(ctx).CreateStitchProject(ctx, &db.CreateStitchProjectParams{
			CreatedBy: userUUID,
			Title:     title,
			Document:  doc,
		})
		if err != nil {
			slog.Error("failed to create stitch project", "error", err)
			return c.String(500, "failed to create project")
		}

		return c.Redirect(302, "/stitch/"+projectID.String())
	}
}

// HandleDeleteProject deletes a stitch project and returns a redirect URL.
func HandleDeleteProject(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.JSON(401, map[string]string{"error": "unauthorized"})
		}
		projectUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		ctx := c.Request().Context()
		err = dbc.Queries(ctx).DeleteStitchProject(ctx, &db.DeleteStitchProjectParams{
			ID:     projectUUID,
			UserID: userUUID,
		})
		if err != nil {
			slog.Error("failed to delete stitch project", "error", err)
			return c.JSON(500, map[string]string{"error": "failed to delete"})
		}

		return c.JSON(200, map[string]string{"redirect": "/stitch"})
	}
}
