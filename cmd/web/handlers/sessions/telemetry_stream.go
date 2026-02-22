package sessions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/internal/producer"
	"thirdcoast.systems/rewind/cmd/web/internal/telemetry"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

type playerTelemetryPost struct {
	RemoteKey  string `json:"remote_key"`
	RTTMs      int    `json:"rtt_ms"`
	JitterMs   int    `json:"jitter_ms"`
	OffsetMs   int    `json:"offset_ms"`
	Visibility string `json:"visibility"`
}

func formatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

func rttClass(rttMs int) string {
	switch {
	case rttMs <= 0:
		return "text-white/40"
	case rttMs <= 120:
		return "text-green-400"
	case rttMs <= 250:
		return "text-yellow-400"
	default:
		return "text-red-400"
	}
}

func jitterClass(jitterMs int) string {
	switch {
	case jitterMs <= 0:
		return "text-white/40"
	case jitterMs <= 30:
		return "text-green-400"
	case jitterMs <= 80:
		return "text-yellow-400"
	default:
		return "text-red-400"
	}
}

// HandleProducerStream returns an SSE handler that streams telemetry events to the producer.
func HandleProducerStream(sm *auth.SessionManager, dbc *db.DatabaseConnection, hub *telemetry.Hub) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Auth required
		_, _, err := sm.GetSession(c.Request())
		if err != nil {
			return c.String(401, "unauthorized")
		}
		code := c.Param("code")
		if len(code) != 6 {
			return c.String(400, "invalid session code")
		}

		_, err = dbc.Queries(c.Request().Context()).GetPlayerSessionByCode(c.Request().Context(), code)
		if err != nil {
			return c.String(404, "session not found")
		}

		if !hub.AcquireProducerStream(code) {
			return c.String(429, "too many open producer streams")
		}
		defer hub.ReleaseProducerStream(code)

		resp := c.Response()
		flusher, ok := resp.Writer.(http.Flusher)
		if !ok {
			return c.String(500, "streaming unsupported")
		}

		common.SetSSEHeaders(c)

		sse := datastar.NewSSE(resp, c.Request())

		// Phase 0: just prove the stream is alive and patch placeholders.
		_ = sse.PatchElementTempl(templates.ProducerSSEStatus("Connected", "text-green-400"), datastar.WithSelectorID("producer-sse-status"), datastar.WithModeReplace())

		remotes := hub.ListRemotes(code)
		_ = sse.PatchElementTempl(templates.ProducerRemoteCount(len(remotes)), datastar.WithSelectorID("remote-count"), datastar.WithModeReplace())
		emptyRowPresent := len(remotes) == 0
		if len(remotes) > 0 {
			_ = sse.PatchElementTempl(templates.Empty(), datastar.WithSelectorID("remote-empty-row"), datastar.WithModeRemove())
			emptyRowPresent = false
			for _, rt := range remotes {
				age := formatAge(time.Since(rt.FirstSeen))
				role := "player/anon"
				if rt.Auth == "user" {
					role = "player/user"
				}
				clientLabel := rt.RemoteID
				_ = sse.PatchElementTempl(
					templates.ProducerRemoteRow(rt.RemoteKey, clientLabel, role, age, rt.RTTMs, rt.JitterMs, rt.OffsetMs, rttClass(rt.RTTMs), jitterClass(rt.JitterMs)),
					datastar.WithSelectorID("remotes-tbody"),
					datastar.WithModeAppend(),
				)
			}
		}

		evtCh, unsubscribe := hub.Subscribe(code)
		defer unsubscribe()

		known := make(map[string]struct{}, len(remotes))
		for _, rt := range remotes {
			known[rt.RemoteKey] = struct{}{}
		}

		// Keep-alive comments so proxies/browsers keep the stream open.
		_, _ = fmt.Fprintf(resp, ": connected\n\n")
		flusher.Flush()

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		pruneTicker := time.NewTicker(2 * time.Second)
		defer pruneTicker.Stop()

		for {
			select {
			case <-c.Request().Context().Done():
				return nil
			case evt, ok := <-evtCh:
				if !ok {
					return nil
				}

				switch evt.Typ {
				case telemetry.EventUpsertRemote:
					// Remove the empty row once (otherwise DataStar will spam PatchElementsNoTargetsFound).
					if emptyRowPresent {
						_ = sse.PatchElementTempl(templates.Empty(), datastar.WithSelectorID("remote-empty-row"), datastar.WithModeRemove())
						emptyRowPresent = false
					}

					// Update count.
					_ = sse.PatchElementTempl(templates.ProducerRemoteCount(len(hub.ListRemotes(code))), datastar.WithSelectorID("remote-count"), datastar.WithModeReplace())

					age := formatAge(time.Since(evt.Telemetry.FirstSeen))
					role := "player/anon"
					if evt.Telemetry.Auth == "user" {
						role = "player/user"
					}
					clientLabel := evt.Telemetry.RemoteID

					row := templates.ProducerRemoteRow(
						evt.Telemetry.RemoteKey,
						clientLabel,
						role,
						age,
						evt.Telemetry.RTTMs,
						evt.Telemetry.JitterMs,
						evt.Telemetry.OffsetMs,
						rttClass(evt.Telemetry.RTTMs),
						jitterClass(evt.Telemetry.JitterMs),
					)

					if _, ok := known[evt.RemoteKey]; ok {
						_ = sse.PatchElementTempl(row, datastar.WithSelectorID("remote-row-"+evt.RemoteKey), datastar.WithModeReplace())
					} else {
						known[evt.RemoteKey] = struct{}{}
						_ = sse.PatchElementTempl(row, datastar.WithSelectorID("remotes-tbody"), datastar.WithModePrepend())
					}

					flusher.Flush()
				case telemetry.EventRemoveRemote:
					delete(known, evt.RemoteKey)
					_ = sse.PatchElementTempl(templates.Empty(), datastar.WithSelectorID("remote-row-"+evt.RemoteKey), datastar.WithModeRemove())

					count := len(hub.ListRemotes(code))
					_ = sse.PatchElementTempl(templates.ProducerRemoteCount(count), datastar.WithSelectorID("remote-count"), datastar.WithModeReplace())
					if count == 0 && !emptyRowPresent {
						_ = sse.PatchElementTempl(templates.ProducerRemoteEmptyRow(code), datastar.WithSelectorID("remotes-tbody"), datastar.WithModeAppend())
						emptyRowPresent = true
					}

					flusher.Flush()
				default:
					continue
				}
			case <-pruneTicker.C:
				// Detect remotes leaving by pruning stale telemetry.
				_ = hub.PruneStale(code, time.Now())
			case <-ticker.C:
				_, _ = fmt.Fprintf(resp, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

// HandlePlayerStream returns an SSE handler that streams scene updates to a remote player.
func HandlePlayerStream(sm *auth.SessionManager, dbc *db.DatabaseConnection, hub *telemetry.Hub, sceneHub *producer.SceneHub) echo.HandlerFunc {
	return func(c echo.Context) error {
		code := c.Param("code")
		if len(code) != 6 {
			return c.String(400, "invalid session code")
		}

		// Remote is unauthenticated; validate session exists.
		session, err := dbc.Queries(c.Request().Context()).GetPlayerSessionByCode(c.Request().Context(), code)
		if err != nil {
			return c.String(404, "session not found")
		}

		if !hub.AcquirePlayerStream(code) {
			return c.String(429, "too many open player streams")
		}
		defer hub.ReleasePlayerStream(code)

		authLevel := "anon"
		userID := ""
		if uid, _, err := sm.GetSession(c.Request()); err == nil {
			authLevel = "user"
			userID = uid
		}

		host, port, _ := net.SplitHostPort(c.Request().RemoteAddr)
		ua := c.Request().UserAgent()
		if host == "" {
			host = c.RealIP()
		}
		if port == "" {
			port = "0"
		}
		// Backend-only stable key for this specific SSE connection.
		name := "code=" + code + "|remote_addr=" + c.Request().RemoteAddr + "|ip=" + host + "|port=" + port + "|user=" + userID + "|ua=" + ua
		remoteKey := uuid.NewSHA1(uuid.NameSpaceURL, []byte(name)).String()

		now := time.Now()
		hub.UpsertRemote(code, telemetry.RemoteTelemetry{
			RemoteKey: remoteKey,
			RemoteID:  remoteKey,
			FirstSeen: now,
			LastSeen:  now,
			Auth:      authLevel,
			UserAgent: ua,
			RemoteIP:  c.RealIP(),
		})
		defer hub.RemoveRemote(code, remoteKey)

		resp := c.Response()
		flusher, ok := resp.Writer.(http.Flusher)
		if !ok {
			return c.String(500, "streaming unsupported")
		}

		common.SetSSEHeaders(c)

		sse := datastar.NewSSE(resp, c.Request())

		_ = sse.PatchElementTempl(templates.PlayerSSEStatus("Connected", "text-green-400"), datastar.WithSelectorID("player-sse-status"), datastar.WithModeReplace())
		_ = sse.PatchElementTempl(templates.PlayerRemoteKey(remoteKey), datastar.WithSelectorID("player-remote-key"), datastar.WithModeReplace())

		sceneJSON := extractSceneFromState(session.State)
		_ = sse.PatchElementTempl(templates.PlayerScene(base64.StdEncoding.EncodeToString(sceneJSON)), datastar.WithSelectorID("player-scene"), datastar.WithModeReplace())

		sceneCh, unsubscribeScene := sceneHub.Subscribe(code)
		defer unsubscribeScene()

		_, _ = fmt.Fprintf(resp, ": connected\n\n")
		flusher.Flush()

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-c.Request().Context().Done():
				return nil
			case scene, ok := <-sceneCh:
				if !ok {
					return nil
				}
				_ = sse.PatchElementTempl(templates.PlayerScene(base64.StdEncoding.EncodeToString(scene)), datastar.WithSelectorID("player-scene"), datastar.WithModeReplace())
				flusher.Flush()
			case <-ticker.C:
				_ = hub.TouchRemote(code, remoteKey, time.Now())
				_, _ = fmt.Fprintf(resp, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

// HandlePlayerTelemetry returns a handler that accepts telemetry POST from remote players.
func HandlePlayerTelemetry(hub *telemetry.Hub) echo.HandlerFunc {
	return func(c echo.Context) error {
		code := c.Param("code")
		if len(code) != 6 {
			return c.String(400, "invalid session code")
		}

		var payload playerTelemetryPost
		if err := json.NewDecoder(c.Request().Body).Decode(&payload); err != nil {
			return c.String(400, "invalid json")
		}
		if payload.RemoteKey == "" {
			return c.String(400, "missing remote_key")
		}

		// Only update remotes that are already known from an open SSE connection.
		_ = hub.UpdateRemoteFromTelemetry(code, payload.RemoteKey, payload.RTTMs, payload.JitterMs, payload.OffsetMs, payload.Visibility, time.Now())
		return c.NoContent(204)
	}
}
