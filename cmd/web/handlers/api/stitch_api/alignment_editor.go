package stitch_api

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"net/http"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

// RegisterEditorAlignment registers the owned alignment request/status routes.
func RegisterEditorAlignment(e *echo.Group, sm *auth.SessionManager, dbc *db.DatabaseConnection) {
	e.POST("/stitch/projects/:id/captions/:captionID/alignment", alignmentRequestHandler(sm, dbc))
	e.GET("/stitch/projects/:id/alignment/:jobID", alignmentStatusHandler(sm, dbc))
}
func alignmentRequestHandler(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, name, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return err
		}
		var q struct {
			ExpectedRevision *int64 `json:"expected_revision"`
			OperationKey     string `json:"operation_key"`
			Language         string `json:"language"`
			ModelVersion     string `json:"model_version"`
		}
		if err = editorJSON(c, &q); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}
		if q.ExpectedRevision == nil || q.OperationKey == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "expected_revision and operation_key are required")
		}
		r, err := stitch.NewStore(dbc).QueueAlignment(c.Request().Context(), owner, project, *q.ExpectedRevision, q.OperationKey, stitch.Actor{Kind: "user", ID: owner.String(), Name: name}, c.Param("captionID"), q.Language, q.ModelVersion)
		if err != nil {
			return editorError(c, err)
		}
		key := ""
		for _, cap := range r.Document.Captions {
			if cap.ID == c.Param("captionID") {
				key = cap.AlignmentKey
				break
			}
		}
		job, err := stitch.NewStore(dbc).AlignmentStatusByKey(c.Request().Context(), owner, project, key)
		if err != nil {
			return editorError(c, err)
		}
		result := snapshotJSON(r.Snapshot)
		result.ChangedIDs, result.EditID, result.Summary = r.ChangedIDs, r.EditID, r.Summary
		return c.JSON(http.StatusAccepted, map[string]any{"result": result, "job_id": job.ID.String(), "alignment_key": key})
	}
}
func alignmentStatusHandler(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, _, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return err
		}
		var job pgtype.UUID
		if err = job.Scan(c.Param("jobID")); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid job id")
		}
		r, err := stitch.NewStore(dbc).AlignmentStatus(c.Request().Context(), owner, project, job)
		if err != nil {
			return editorError(c, err)
		}
		return c.JSON(http.StatusOK, r)
	}
}
