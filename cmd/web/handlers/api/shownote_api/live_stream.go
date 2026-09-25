package shownote_api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/internal/producer"
	"thirdcoast.systems/rewind/cmd/web/internal/scene"
	"thirdcoast.systems/rewind/cmd/web/internal/telemetry"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/shownote"
)

// HandleLiveProducerStream streams the live viewer count to the producer. Auth +
// host required. Keyed by the show note id (the show note IS the session).
func HandleLiveProducerStream(sm *auth.SessionManager, dbc *db.DatabaseConnection, hub *telemetry.Hub) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, _, err := requireEditor(c, sm, dbc)
		if err != nil {
			return c.String(401, "unauthorized")
		}
		key := noteUUID.String()
		if !hub.AcquireProducerStream(key) {
			return c.String(429, "too many producer streams")
		}
		defer hub.ReleaseProducerStream(key)

		resp := c.Response()
		flusher, ok := resp.Writer.(http.Flusher)
		if !ok {
			return c.String(500, "streaming unsupported")
		}
		common.SetSSEHeaders(c)
		sse := datastar.NewSSE(resp, c.Request())

		patchHosts := func() {
			if hosts, err := dbc.Queries(c.Request().Context()).ListActiveConnections(c.Request().Context(), noteUUID); err == nil {
				_ = sse.PatchElementTempl(templates.LiveHostList(hosts, key))
			}
		}

		_ = sse.PatchElementTempl(templates.LiveSSEStatus("Live control connected", "text-green-400"))
		_ = sse.PatchElementTempl(templates.LiveViewerCount(len(hub.ListRemotes(key))))
		patchHosts()

		ch, unsubscribe := hub.Subscribe(key)
		defer unsubscribe()

		_, _ = fmt.Fprintf(resp, ": connected\n\n")
		flusher.Flush()

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		pruneTicker := time.NewTicker(3 * time.Second)
		defer pruneTicker.Stop()

		for {
			select {
			case <-c.Request().Context().Done():
				return nil
			case _, ok := <-ch:
				if !ok {
					return nil
				}
				_ = sse.PatchElementTempl(templates.LiveViewerCount(len(hub.ListRemotes(key))))
				flusher.Flush()
			case <-pruneTicker.C:
				_ = hub.PruneStale(key, time.Now())
			case <-ticker.C:
				_ = sse.PatchElementTempl(templates.LiveViewerCount(len(hub.ListRemotes(key))))
				patchHosts()
				_, _ = fmt.Fprintf(resp, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

// HandleLiveSceneStream streams the active scene to a viewer (or the producer's
// own preview) and registers the connection as a remote so the producer sees the
// viewer count. The UUID capability is valid only while live. Editors may
// preview offline; permissions are rechecked before updates and on heartbeat.
func HandleLiveSceneStream(sm *auth.SessionManager, dbc *db.DatabaseConnection, hub *telemetry.Hub, sceneHub *producer.SceneHub) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		key := noteUUID.String()

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		candidate, err := q.GetShowNote(ctx, noteUUID)
		if err != nil {
			return c.String(404, "not found")
		}
		publicViewer := candidate.IsLive
		loadNote := func() (*db.ShowNote, error) {
			if publicViewer {
				// The public viewer route is capability-based: the unguessable
				// note UUID may subscribe while the show is live, without a
				// workspace session. Authenticated editor requests remain scoped.
				return dbc.Queries(ctx).GetShowNote(ctx, noteUUID)
			}
			return shownote.RequireTenant(ctx, dbc, noteUUID)
		}
		note, err := loadNote()
		if err != nil {
			return c.String(404, "not found")
		}
		mayView := func(note *db.ShowNote) bool {
			if note.IsLive {
				return true
			}
			uid, _, err := sm.GetSession(c.Request())
			var userID pgtype.UUID
			return err == nil && userID.Scan(uid) == nil && canEditShowNote(c.Request().Context(), dbc, noteUUID, userID)
		}
		if !mayView(note) {
			return c.String(http.StatusForbidden, "show is offline")
		}
		stillAllowed := func() bool {
			current, err := loadNote()
			return err == nil && mayView(current)
		}

		if !hub.AcquirePlayerStream(key) {
			return c.String(429, "too many viewers")
		}
		defer hub.ReleasePlayerStream(key)

		authLevel := "anon"
		userID := ""
		if uid, _, err := sm.GetSession(c.Request()); err == nil {
			authLevel = "user"
			userID = uid
		}
		name := "note=" + key + "|addr=" + c.Request().RemoteAddr + "|user=" + userID + "|ua=" + c.Request().UserAgent()
		remoteKey := uuid.NewSHA1(uuid.NameSpaceURL, []byte(name)).String()

		now := time.Now()
		hub.UpsertRemote(key, telemetry.RemoteTelemetry{
			RemoteKey: remoteKey, RemoteID: remoteKey, FirstSeen: now, LastSeen: now,
			Auth: authLevel, UserAgent: c.Request().UserAgent(), RemoteIP: c.RealIP(),
		})
		defer hub.RemoveRemote(key, remoteKey)

		resp := c.Response()
		flusher, ok := resp.Writer.(http.Flusher)
		if !ok {
			return c.String(500, "streaming unsupported")
		}
		common.SetSSEHeaders(c)
		sse := datastar.NewSSE(resp, c.Request())

		_ = sse.PatchElementTempl(templates.LiveSSEStatus("Connected", "text-green-400"))
		_ = sse.PatchElementTempl(templates.LiveRemoteKey(remoteKey))
		_ = sse.PatchElementTempl(templates.LiveSceneEl(scene.ToBase64(scene.FromState(note.SceneState))))

		sceneCh, unsubscribe := sceneHub.Subscribe(key)
		defer unsubscribe()

		_, _ = fmt.Fprintf(resp, ": connected\n\n")
		flusher.Flush()

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-c.Request().Context().Done():
				return nil
			case sceneJSON, ok := <-sceneCh:
				if !ok || !stillAllowed() {
					return nil
				}
				_ = sse.PatchElementTempl(templates.LiveSceneEl(scene.ToBase64(sceneJSON)))
				flusher.Flush()
			case <-ticker.C:
				if !stillAllowed() {
					return nil
				}
				_ = hub.TouchRemote(key, remoteKey, time.Now())
				_, _ = fmt.Fprintf(resp, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

// HandleLiveTelemetryPost accepts viewer-reported metrics (keeps the remote alive).
func HandleLiveTelemetryPost(hub *telemetry.Hub) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		var payload struct {
			RemoteKey  string `json:"remote_key"`
			RTTMs      int    `json:"rtt_ms"`
			JitterMs   int    `json:"jitter_ms"`
			OffsetMs   int    `json:"offset_ms"`
			Visibility string `json:"visibility"`
		}
		if err := c.Bind(&payload); err != nil || payload.RemoteKey == "" {
			return c.NoContent(204)
		}
		_ = hub.UpdateRemoteFromTelemetry(noteUUID.String(), payload.RemoteKey, payload.RTTMs, payload.JitterMs, payload.OffsetMs, payload.Visibility, time.Now())
		return c.NoContent(204)
	}
}
