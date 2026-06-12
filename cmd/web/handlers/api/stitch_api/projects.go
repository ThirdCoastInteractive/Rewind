package stitch_api

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleCreateProject creates a new stitch project and redirects to its editor.
func HandleCreateProject(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		ctx := c.Request().Context()
		projectID, err := dbc.Queries(ctx).CreateStitchProject(ctx, &db.CreateStitchProjectParams{
			CreatedBy: userUUID,
			Title:     "Untitled",
		})
		if err != nil {
			slog.Error("failed to create stitch project", "error", err)
			return c.String(500, "failed to create project")
		}

		return c.Redirect(302, "/stitch/"+projectID.String())
	}
}

// HandleSaveProject saves the current editor state to a stitch project.
func HandleSaveProject(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.JSON(401, map[string]string{"error": "unauthorized"})
		}
		projectUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		var req struct {
			Title    string            `json:"title"`
			Format   string            `json:"format"`
			Quality  string            `json:"quality"`
			Segments []json.RawMessage `json:"segments"`
		}
		if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
			return c.JSON(400, map[string]string{"error": "invalid request body"})
		}

		format := strings.TrimSpace(req.Format)
		if format == "" {
			format = "mp4"
		}
		quality := strings.TrimSpace(req.Quality)
		if quality == "" {
			quality = "high"
		}

		segmentsJSON, _ := json.Marshal(req.Segments)
		if len(req.Segments) == 0 {
			segmentsJSON = []byte("[]")
		}

		ctx := c.Request().Context()
		err = dbc.Queries(ctx).UpdateStitchProject(ctx, &db.UpdateStitchProjectParams{
			ID:            projectUUID,
			UserID:        userUUID,
			Title:         strings.TrimSpace(req.Title),
			Format:        format,
			Quality:       quality,
			Segments:      segmentsJSON,
			GlobalFilters: []byte("[]"),
		})
		if err != nil {
			slog.Error("failed to save stitch project", "error", err)
			return c.JSON(500, map[string]string{"error": "failed to save"})
		}

		return c.JSON(200, map[string]string{"status": "ok"})
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

// HandleProjectExports returns the export history for a project via SSE patch.
func HandleProjectExports(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.String(401, "unauthorized")
		}
		projectUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		ctx := c.Request().Context()
		jobs, err := dbc.Queries(ctx).ListStitchJobsByProject(ctx, projectUUID)
		if err != nil {
			slog.Error("failed to list project exports", "error", err)
			return c.String(500, "failed to load exports")
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		return sse.PatchElementTempl(components.StitchExportHistory(jobs))
	}
}
