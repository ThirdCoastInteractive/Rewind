package stitch_api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

// RegisterEditorMedia registers authenticated project asset upload and retrieval routes.
func RegisterEditorMedia(e *echo.Group, sm *auth.SessionManager, dbc *db.DatabaseConnection, exportsDir string) {
	e.POST("/stitch/projects/:id/assets", editorAssetUpload(sm, dbc, exportsDir))
	e.GET("/stitch/projects/:id/assets/:assetID", editorAssetGet(sm, dbc))
	e.POST("/stitch/projects/:id/captions/import", editorCaptionImport(sm, dbc))
}

func editorCaptionImport(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, name, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return err
		}
		var req struct {
			ExpectedRevision *int64 `json:"expected_revision"`
			OperationKey     string `json:"operation_key"`
			SegmentID        string `json:"segment_id"`
			Language         string `json:"language"`
		}
		if err := editorJSON(c, &req); err != nil {
			return err
		}
		if req.ExpectedRevision == nil || strings.TrimSpace(req.OperationKey) == "" || len(req.OperationKey) > 200 || req.SegmentID == "" || req.Language == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "revision, operation key, segment and language are required")
		}
		result, err := stitch.NewStore(dbc).ImportCaptions(c.Request().Context(), owner, project, *req.ExpectedRevision, req.OperationKey, stitch.Actor{Kind: "user", ID: owner.String(), Name: name}, req.SegmentID, req.Language)
		if err != nil {
			return editorError(c, err)
		}
		response := snapshotJSON(result.Snapshot)
		response.ChangedIDs, response.EditID, response.Summary = result.ChangedIDs, result.EditID, result.Summary
		return c.JSON(http.StatusOK, response)
	}
}
func editorAssetUpload(sm *auth.SessionManager, dbc *db.DatabaseConnection, exportsDir string) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, 21<<20)
		owner, _, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return err
		}
		fh, err := c.FormFile("file")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "file is required")
		}
		if c.Request().MultipartForm != nil {
			defer c.Request().MultipartForm.RemoveAll()
		}
		f, err := fh.Open()
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid file")
		}
		defer f.Close()
		a, err := stitch.NewStore(dbc).AddAsset(c.Request().Context(), owner, project, f, fh.Header.Get("Content-Type"), exportsDir)
		if err != nil {
			return editorError(c, err)
		}
		a.Path = ""
		return c.JSON(http.StatusCreated, a)
	}
}
func editorAssetGet(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, _, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return err
		}
		id, err := common.RequireUUIDParam(c, "assetID")
		if err != nil {
			return err
		}
		a, err := stitch.NewStore(dbc).GetAsset(c.Request().Context(), owner, project, id)
		if err != nil {
			return editorError(c, err)
		}
		if !filepath.IsAbs(a.Path) {
			return echo.NewHTTPError(http.StatusNotFound, "asset unavailable")
		}
		if _, err = os.Stat(a.Path); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "asset unavailable")
		}
		return c.File(a.Path)
	}
}
