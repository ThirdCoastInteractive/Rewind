package stitch_api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

type renderRequest struct {
	ExpectedRevision *int64 `json:"revision"`
	OperationKey     string `json:"operation_key"`
	stitch.RenderOptions
}

func registerEditorRenderRoutes(e *echo.Group, sm *auth.SessionManager, dbc *db.DatabaseConnection) {
	e.POST("/stitch/projects/:id/exports", editorExport(sm, dbc))
	e.POST("/stitch/projects/:id/previews", editorPreview(sm, dbc))
	e.POST("/stitch/projects/:id/frames", editorFrame(sm, dbc))
	e.GET("/stitch/projects/:id/render-jobs", editorRenderJobs(sm, dbc))
	e.GET("/stitch/projects/:id/render-jobs/:jobID", editorRenderStatus(sm, dbc))
}

func validRenderRequest(req *renderRequest) error {
	if req == nil || req.ExpectedRevision == nil || *req.ExpectedRevision < 0 {
		return fmt.Errorf("revision is required")
	}
	if strings.TrimSpace(req.OperationKey) == "" || len(req.OperationKey) > 200 {
		return fmt.Errorf("bounded operation_key is required")
	}
	if strings.TrimSpace(req.CaptionMode) == "" {
		req.CaptionMode = "none"
	}
	if strings.TrimSpace(req.Scope) == "" {
		req.Scope = "all"
	}
	return nil
}

func renderJobResponse(job stitch.RenderJob) map[string]any {
	kind := job.Kind
	if kind == "" {
		kind = "export"
	}
	result := map[string]any{
		"id": job.ID, "project_id": job.ProjectID, "revision": job.Revision,
		"kind": kind, "status": job.Status, "options": job.Options,
		"mime": job.MIME, "progress": job.Progress, "progress_pct": job.Progress,
	}
	if job.Error != "" {
		result["error"] = job.Error
	}
	if len(job.SidecarURLs) > 0 {
		result["sidecar_urls"] = job.SidecarURLs
	}
	if job.AssetURL != "" {
		result["asset_url"] = "/api/stitch/" + job.ID.String() + "/stream"
	}
	return result
}

func queueRenderRequest(c echo.Context, sm *auth.SessionManager, dbc *db.DatabaseConnection, kind string) error {
	owner, _, err := editorUser(c, sm, dbc)
	if err != nil {
		return err
	}
	project, err := editorID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid project")
	}
	var req renderRequest
	if err := editorJSON(c, &req); err != nil {
		return err
	}
	if err := validRenderRequest(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if kind == "frame" && req.FrameTimeUS < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "frame_time_us is required")
	}
	store := stitch.NewStore(dbc)
	var job stitch.RenderJob
	switch kind {
	case "preview":
		job, err = store.QueuePreview(c.Request().Context(), owner, project, *req.ExpectedRevision, req.OperationKey, req.RenderOptions)
	case "frame":
		job, err = store.QueueFrame(c.Request().Context(), owner, project, *req.ExpectedRevision, req.OperationKey, req.RenderOptions)
	default:
		job, err = store.QueueExport(c.Request().Context(), owner, project, *req.ExpectedRevision, req.OperationKey, req.RenderOptions)
	}
	if err != nil {
		return editorError(c, err)
	}
	return c.JSON(http.StatusOK, renderJobResponse(job))
}

func editorExport(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error { return queueRenderRequest(c, sm, dbc, "export") }
}

func editorPreview(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error { return queueRenderRequest(c, sm, dbc, "preview") }
}

func editorFrame(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error { return queueRenderRequest(c, sm, dbc, "frame") }
}

func editorRenderJobs(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, _, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid project")
		}
		jobs, err := stitch.NewStore(dbc).ListRenderJobs(c.Request().Context(), owner, project)
		if err != nil {
			return editorError(c, err)
		}
		out := make([]map[string]any, 0, len(jobs))
		for _, job := range jobs {
			out = append(out, renderJobResponse(job))
		}
		return c.JSON(http.StatusOK, map[string]any{"jobs": out})
	}
}

func editorRenderStatus(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, _, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid project")
		}
		jobID, err := common.RequireUUIDParam(c, "jobID")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid job")
		}
		job, err := stitch.NewStore(dbc).GetRenderJob(c.Request().Context(), owner, jobID)
		if err != nil {
			return editorError(c, err)
		}
		if job.ProjectID != project {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "render job not found"})
		}
		return c.JSON(http.StatusOK, renderJobResponse(job))
	}
}
