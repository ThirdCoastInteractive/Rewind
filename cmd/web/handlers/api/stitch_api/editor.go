package stitch_api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/events"
	"thirdcoast.systems/rewind/internal/stitch"
)

// RegisterEditor registers canonical Stitch document, command, history, and event routes.
func RegisterEditor(e *echo.Group, sm *auth.SessionManager, dbc *db.DatabaseConnection) {
	registerEditorRenderRoutes(e, sm, dbc)
	RegisterEditorAlignment(e, sm, dbc)
	e.GET("/stitch/projects/:id/document", editorDocument(sm, dbc))
	e.PUT("/stitch/projects/:id/youtube", editorYouTube(sm, dbc))
	e.POST("/stitch/projects/:id/commands", editorCommands(sm, dbc))
	e.POST("/stitch/projects/:id/undo", editorUndo(sm, dbc))
	e.POST("/stitch/projects/:id/redo", editorRedo(sm, dbc))
	e.GET("/stitch/projects/:id/history", editorHistory(sm, dbc))
	e.GET("/stitch/projects/:id/events", editorEvents(sm, dbc))
}

type editorRequest struct {
	ExpectedRevision *int64             `json:"expected_revision"`
	OperationKey     string             `json:"operation_key"`
	Summary          string             `json:"summary"`
	Operations       []stitch.Operation `json:"operations"`
}
type editorResponse struct {
	stitch.Snapshot
	Resolved         []stitch.ResolvedSegment `json:"resolved"`
	ResolvedCaptions []stitch.Caption         `json:"resolved_captions"`
	ChangedIDs       []string                 `json:"changed_ids,omitempty"`
	EditID           pgtype.UUID              `json:"edit_id,omitempty"`
	Summary          string                   `json:"summary,omitempty"`
}

func editorUser(c echo.Context, sm *auth.SessionManager, dbc *db.DatabaseConnection) (pgtype.UUID, string, error) {
	uid, username, err := common.RequireSessionUser(c, sm)
	if err != nil {
		return uid, username, err
	}
	user, err := dbc.Queries(c.Request().Context()).SelectUserByID(c.Request().Context(), uid)
	if err != nil || user == nil || !user.Enabled {
		return pgtype.UUID{}, "", echo.NewHTTPError(http.StatusUnauthorized, "account disabled")
	}
	return uid, username, nil
}
func editorID(c echo.Context) (pgtype.UUID, error) { return common.RequireUUIDParam(c, "id") }
func editorJSON(c echo.Context, v any) error {
	c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, 2<<20)
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return echo.NewHTTPError(http.StatusBadRequest, "request body must contain one JSON value")
	}
	return nil
}
func snapshotJSON(s stitch.Snapshot) editorResponse {
	return editorResponse{Snapshot: s, Resolved: stitch.Resolve(s.Document), ResolvedCaptions: stitch.ResolvedCaptions(s.Document)}
}
func editorError(c echo.Context, err error) error {
	var ce *stitch.ConflictError
	var databaseError *pgconn.PgError
	switch {
	case errors.As(err, &databaseError):
		c.Logger().Error(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "The editor could not save this request. Please retry."})
	case errors.As(err, &ce):
		return c.JSON(http.StatusConflict, map[string]any{"error": "revision conflict", "current_revision": ce.CurrentRevision})
	case errors.Is(err, stitch.ErrNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "project not found"})
	case errors.Is(err, stitch.ErrDisabled):
		return c.JSON(http.StatusConflict, map[string]string{"error": "editor is disabled"})
	case errors.Is(err, stitch.ErrIdempotency):
		return c.JSON(http.StatusConflict, map[string]string{"error": "operation key was reused"})
	default:
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func editorDocument(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, _, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return err
		}
		snap, err := stitch.NewStore(dbc).Get(c.Request().Context(), owner, project)
		if err != nil {
			return editorError(c, err)
		}
		return c.JSON(http.StatusOK, snapshotJSON(snap))
	}
}
func editorCommands(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, username, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return err
		}
		var req editorRequest
		if err := editorJSON(c, &req); err != nil {
			return err
		}
		if req.ExpectedRevision == nil || *req.ExpectedRevision < 0 || strings.TrimSpace(req.OperationKey) == "" || strings.TrimSpace(req.Summary) == "" || len(req.Summary) > 240 || len(req.OperationKey) > 200 || len(req.Operations) == 0 || len(req.Operations) > 100 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "expected_revision, operation_key, summary, and 1-100 operations are required"})
		}
		actor := stitch.Actor{Kind: "user", ID: owner.String(), Name: username}
		result, err := stitch.NewStore(dbc).Commit(c.Request().Context(), owner, project, *req.ExpectedRevision, req.OperationKey, actor, req.Summary, req.Operations)
		if err != nil {
			return editorError(c, err)
		}
		response := snapshotJSON(result.Snapshot)
		response.ChangedIDs = result.ChangedIDs
		response.EditID = result.EditID
		response.Summary = result.Summary
		return c.JSON(http.StatusOK, response)
	}
}
func editorHistoryMove(sm *auth.SessionManager, dbc *db.DatabaseConnection, redo bool) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, username, err := editorUser(c, sm, dbc)
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
		}
		if err := editorJSON(c, &req); err != nil {
			return err
		}
		if req.ExpectedRevision == nil || *req.ExpectedRevision < 0 || len(req.OperationKey) > 200 || req.OperationKey == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "expected_revision and operation_key are required"})
		}
		actor := stitch.Actor{Kind: "user", ID: owner.String(), Name: username}
		store := stitch.NewStore(dbc)
		var result stitch.Result
		if redo {
			result, err = store.Redo(c.Request().Context(), owner, project, *req.ExpectedRevision, req.OperationKey, actor, "Redo edit")
		} else {
			result, err = store.Undo(c.Request().Context(), owner, project, *req.ExpectedRevision, req.OperationKey, actor, "Undo edit")
		}
		if err != nil {
			return editorError(c, err)
		}
		response := snapshotJSON(result.Snapshot)
		response.ChangedIDs = result.ChangedIDs
		response.EditID = result.EditID
		response.Summary = result.Summary
		return c.JSON(http.StatusOK, response)
	}
}
func editorUndo(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return editorHistoryMove(sm, dbc, false)
}
func editorRedo(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return editorHistoryMove(sm, dbc, true)
}
func editorHistory(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, _, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return err
		}
		after := int64(0)
		if raw := c.QueryParam("after_revision"); raw != "" {
			if _, scanErr := fmt.Sscan(raw, &after); scanErr != nil || after < 0 {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid after_revision"})
			}
		}
		limit := 100
		if raw := c.QueryParam("limit"); raw != "" {
			if _, scanErr := fmt.Sscan(raw, &limit); scanErr != nil || limit < 1 || limit > 100 {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "limit must be 1-100"})
			}
		}
		rows, err := stitch.NewStore(dbc).History(c.Request().Context(), owner, project, after, limit)
		if err != nil {
			return editorError(c, err)
		}
		return c.JSON(http.StatusOK, map[string]any{"edits": rows})
	}
}

func editorEvents(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, _, err := editorUser(c, sm, dbc)
		if err != nil {
			return err
		}
		project, err := editorID(c)
		if err != nil {
			return err
		}
		wake, unsubscribe := events.Default.Subscribe("stitch_projects_changed")
		defer unsubscribe()
		ctx := c.Request().Context()
		if _, err := stitch.NewStore(dbc).Get(ctx, owner, project); err != nil {
			return editorError(c, err)
		}
		flusher, ok := c.Response().Writer.(http.Flusher)
		if !ok {
			return echo.NewHTTPError(http.StatusInternalServerError, "streaming unavailable")
		}
		c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
		c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
		c.Response().Header().Set("Connection", "keep-alive")
		c.Response().WriteHeader(http.StatusOK)
		write := func() error {
			snap, getErr := stitch.NewStore(dbc).Get(ctx, owner, project)
			if getErr != nil {
				return getErr
			}
			payload, marshalErr := json.Marshal(snapshotJSON(snap))
			if marshalErr != nil {
				return marshalErr
			}
			_, writeErr := fmt.Fprintf(c.Response(), "event: project\ndata: %s\n\n", payload)
			flusher.Flush()
			return writeErr
		}
		if err := write(); err != nil {
			return nil
		}
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if err := write(); err != nil {
					return nil
				}
			case <-wake:
				if err := write(); err != nil {
					return nil
				}
			}
		}
	}
}
