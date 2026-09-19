//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/stitch_api"
	"thirdcoast.systems/rewind/internal/events"
	"thirdcoast.systems/rewind/internal/stitch"
)

func TestStitchHTTPEventsAuthoritativeProjectSnapshots(t *testing.T) {
	d, owner, project := openStitchTest(t)
	_, foreign, _ := openStitchTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := stitch.NewStore(d)
	initial, err := store.Enable(ctx, owner, project)
	if err != nil {
		t.Fatal(err)
	}

	events.Default.Start(ctx, d)
	sm := auth.NewSessionManager("disposable-stitch-events-fixture")
	e := echo.New()
	stitch_api.RegisterEditor(e.Group("/api"), sm, d)
	server := httptest.NewServer(e)
	defer server.Close()

	withSession := func(req *http.Request, user string) {
		t.Helper()
		cookies := httptest.NewRecorder()
		if err := sm.SaveSession(cookies, req, user, user, auth.AccessUser); err != nil {
			t.Fatal(err)
		}
		for _, cookie := range cookies.Result().Cookies() {
			req.AddCookie(cookie)
		}
	}
	path := server.URL + "/api/stitch/projects/" + project.String() + "/events"
	foreignReq, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	withSession(foreignReq, foreign.String())
	foreignResp, err := http.DefaultClient.Do(foreignReq)
	if err != nil {
		t.Fatal(err)
	}
	foreignResp.Body.Close()
	if foreignResp.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign stream status=%d", foreignResp.StatusCode)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	withSession(req, owner.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream response status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(resp.Body)
	readEvent := func() (map[string]any, error) {
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return nil, readErr
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(bytes.TrimSpace([]byte(strings.TrimPrefix(line, "data: "))), &payload); err != nil {
				return nil, err
			}
			return payload, nil
		}
	}
	first, err := readEvent()
	if err != nil {
		t.Fatal(err)
	}
	if got := int64(first["revision"].(float64)); got != initial.Revision {
		t.Fatalf("initial revision=%d want=%d", got, initial.Revision)
	}

	result, err := store.Commit(ctx, owner, project, initial.Revision, "sse-title", stitch.Actor{Kind: "user", ID: owner.String()}, "SSE title", []stitch.Operation{{Type: "set_title", Title: "SSE authoritative"}})
	if err != nil {
		t.Fatal(err)
	}
	updatedCh := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		payload, readErr := readEvent()
		if readErr != nil {
			errCh <- readErr
			return
		}
		updatedCh <- payload
	}()
	select {
	case updated := <-updatedCh:
		if got := int64(updated["revision"].(float64)); got != result.Revision {
			t.Fatalf("updated revision=%d want=%d", got, result.Revision)
		}
		doc, ok := updated["document"].(map[string]any)
		if !ok || doc["title"] != "SSE authoritative" {
			t.Fatalf("updated document=%v", updated["document"])
		}
	case readErr := <-errCh:
		t.Fatal(readErr)
	case <-time.After(5 * time.Second):
		t.Fatal(fmt.Errorf("timed out waiting for authoritative project event"))
	}
}
