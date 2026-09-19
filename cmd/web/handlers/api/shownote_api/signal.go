package shownote_api

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

var signalUpgrader = websocket.Upgrader{
	// Same-origin in practice; the SFU sits on the internal network behind this proxy.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// HandleSignalProxy authenticates a producer/host for a show note and
// reverse-proxies the WebRTC signaling WebSocket to the SFU service. The SFU
// handles media only; this proxy is where access control lives. (Unauthenticated
// viewer access via the public code is added with the viewer page in a later phase.)
func HandleSignalProxy(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		role := c.QueryParam("role")
		if role == "" {
			role = "host"
		}
		// kind distinguishes a host's camera connection from a screen-share
		// publisher ("camera" | "screen"); it only rides through to the SFU.
		kind := c.QueryParam("kind")

		ctx := c.Request().Context()
		userID := ""
		username := ""
		if role == "viewer" {
			// Viewers (program output / OBS) join unauthenticated to subscribe to
			// host tracks, but only while the show is live. The note id — reached
			// via the public viewer page — is the access capability.
			note, err := dbc.Queries(ctx).GetShowNote(ctx, noteUUID)
			if err != nil || !note.IsLive {
				return c.String(403, "show is not live")
			}
		} else {
			userUUID, name, err := common.RequireSessionUser(c, sm)
			if err != nil {
				return c.String(401, "unauthorized")
			}
			if !canEditShowNote(ctx, dbc, noteUUID, userUUID) {
				return c.String(403, "forbidden")
			}
			userID = userUUID.String()
			username = name
		}

		sfuURL := os.Getenv("SFU_SIGNAL_URL")
		if sfuURL == "" {
			sfuURL = "ws://127.0.0.1:8081/signal"
		}
		// user/username identify the publisher so the SFU can relay a stable
		// stream→host mapping to subscribers (stable webcam slotting + labels).
		target := sfuURL + "?room=" + noteUUID.String() +
			"&user=" + url.QueryEscape(userID) +
			"&username=" + url.QueryEscape(username) +
			"&role=" + url.QueryEscape(role) +
			"&kind=" + url.QueryEscape(kind)

		clientConn, err := signalUpgrader.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			return nil // Upgrade already wrote the error response.
		}
		defer clientConn.Close()

		backendConn, _, err := websocket.DefaultDialer.Dial(target, nil)
		if err != nil {
			slog.Error("signal proxy: dial sfu failed", "error", err, "target", sfuURL)
			_ = clientConn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "sfu unavailable"))
			return nil
		}
		defer backendConn.Close()

		// Pipe both directions. Each connection has exactly one reader and one
		// writer goroutine, which gorilla/websocket permits.
		var once sync.Once
		done := make(chan struct{})
		closeBoth := func() {
			once.Do(func() {
				close(done)
				_ = clientConn.Close()
				_ = backendConn.Close()
			})
		}
		go pipeWS(clientConn, backendConn, closeBoth)
		go pipeWS(backendConn, clientConn, closeBoth)
		<-done
		return nil
	}
}

// pipeWS forwards every message read from src to dst until either side errors.
func pipeWS(dst, src *websocket.Conn, closeBoth func()) {
	for {
		mt, msg, err := src.ReadMessage()
		if err != nil {
			closeBoth()
			return
		}
		if err := dst.WriteMessage(mt, msg); err != nil {
			closeBoth()
			return
		}
	}
}
