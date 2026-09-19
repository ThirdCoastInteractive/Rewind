package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	ygowebsocket "github.com/reearth/ygo/provider/websocket"
)

func TestCollaborationRevocationClosesExistingSocket(t *testing.T) {
	var allowed atomic.Bool
	allowed.Store(true)
	server := ygowebsocket.NewServer()
	server.Authorize = func(r *http.Request) (ygowebsocket.ConnectionConfig, bool) {
		return ygowebsocket.ConnectionConfig{}, allowed.Load() && r.Context().Err() == nil
	}
	defer server.Shutdown(context.Background())
	access := &collaborationAccess{}
	httpServer := httptest.NewServer(access.wrap(server))
	defer httpServer.Close()
	url := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/room"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	allowed.Store(false)
	access.revoke("room")
	for {
		_, _, err = conn.ReadMessage()
		if err != nil {
			break
		}
	}
	if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
		t.Fatal("revoked socket remained open")
	}
	if next, response, err := websocket.DefaultDialer.Dial(url, nil); err == nil {
		next.Close()
		t.Fatal("revoked user reconnected")
	} else if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected reconnect result: %v %v", response, err)
	}
}

func TestCollaborationRevocationIncludesPendingAuthorization(t *testing.T) {
	access := &collaborationAccess{}
	entered, done := make(chan struct{}), make(chan struct{})
	handler := access.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(done)
	}))
	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/room", nil))
	<-entered
	access.revoke("room")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pending upgrade escaped revocation")
	}
}
