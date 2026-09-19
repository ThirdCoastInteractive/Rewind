package shownote_api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/internal/producer"
	"thirdcoast.systems/rewind/cmd/web/internal/scene"
	"thirdcoast.systems/rewind/internal/db"
)

// randomCode returns a 6-digit numeric code for the public viewer URL.
func randomCode() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	n := (uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])) % 1000000
	return fmt.Sprintf("%06d", n)
}

// HandleGoLive marks a show note live, mints a unique public viewer code, seeds
// the active scene, broadcasts it, and returns the live state to the producer.
func HandleGoLive(sm *auth.SessionManager, dbc *db.DatabaseConnection, sceneHub *producer.SceneHub) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, _, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		note, err := q.GetShowNote(ctx, noteUUID)
		if err != nil {
			return echo.NewHTTPError(404, "not found")
		}

		code := note.PublicCode
		if code == nil || *code == "" {
			for i := 0; i < 6; i++ {
				cand := randomCode()
				if _, err := q.GetShowNoteByPublicCode(ctx, &cand); err != nil {
					code = &cand // free code
					break
				}
			}
		}

		live := true
		if _, err := q.UpdateShowNote(ctx, &db.UpdateShowNoteParams{
			ID:            noteUUID,
			IsLive:        &live,
			PublicCode:    code,
			LiveStartedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}); err != nil {
			slog.Error("go live failed", "error", err)
			return echo.NewHTTPError(500, "go live failed")
		}

		sceneJSON := scene.FromState(note.SceneState)
		_ = q.SetShowNoteScene(ctx, &db.SetShowNoteSceneParams{ID: noteUUID, SceneState: sceneJSON})
		sceneHub.Broadcast(noteUUID.String(), sceneJSON)

		codeStr := ""
		if code != nil {
			codeStr = *code
		}
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		return sse.PatchSignals([]byte(fmt.Sprintf(`{"_pLive":true,"_pCode":%q}`, codeStr)))
	}
}

// HandleEndLive takes a show note offline.
func HandleEndLive(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, _, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		live := false
		if _, err := dbc.Queries(c.Request().Context()).UpdateShowNote(c.Request().Context(), &db.UpdateShowNoteParams{ID: noteUUID, IsLive: &live}); err != nil {
			return echo.NewHTTPError(500, "failed")
		}
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		return sse.PatchSignals([]byte(`{"_pLive":false}`))
	}
}

// HandleApplyScene builds a scene from the producer's signals (sent as the @post
// payload), persists it to the show note, and broadcasts it to all viewers.
func HandleApplyScene(sm *auth.SessionManager, dbc *db.DatabaseConnection, sceneHub *producer.SceneHub) echo.HandlerFunc {
	return func(c echo.Context) error {
		// ReadSignals must run before NewSSE (it consumes the request body).
		var p scene.Params
		_ = datastar.ReadSignals(c.Request(), &p)

		noteUUID, _, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		sceneJSON := scene.Build(p, time.Now().UnixMilli())
		if err := dbc.Queries(c.Request().Context()).SetShowNoteScene(c.Request().Context(), &db.SetShowNoteSceneParams{ID: noteUUID, SceneState: sceneJSON}); err != nil {
			return echo.NewHTTPError(500, "apply failed")
		}
		sceneHub.Broadcast(noteUUID.String(), sceneJSON)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		_ = sse
		return nil
	}
}

// HandleSetScene stores a full v3 scene collection (sent as the raw JSON request
// body by the producer's scene editor) on the show note and broadcasts it to all
// viewers and other hosts. The producer renders locally for instant feedback, so
// this is the persistence + fan-out path. The body's optional top-level `_origin`
// lets the sending client suppress its own echo.
func HandleSetScene(sm *auth.SessionManager, dbc *db.DatabaseConnection, sceneHub *producer.SceneHub) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, _, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(io.LimitReader(c.Request().Body, 2<<20))
		if err != nil || !json.Valid(body) {
			return echo.NewHTTPError(400, "invalid scene")
		}
		ctx := c.Request().Context()
		if err := dbc.Queries(ctx).SetShowNoteScene(ctx, &db.SetShowNoteSceneParams{ID: noteUUID, SceneState: body}); err != nil {
			return echo.NewHTTPError(500, "store failed")
		}
		sceneHub.Broadcast(noteUUID.String(), body)
		return c.NoContent(200)
	}
}
