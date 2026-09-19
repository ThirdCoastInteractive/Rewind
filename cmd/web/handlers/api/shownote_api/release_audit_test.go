//go:build releaseaudit

package shownote_api

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/internal/producer"
	"thirdcoast.systems/rewind/cmd/web/internal/telemetry"
	"thirdcoast.systems/rewind/internal/db"
	"time"
)

func TestOfflineSceneRejectsAnonymousViewer(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbc := &db.DatabaseConnection{Pool: pool}
	user, note := uuid.NewString(), uuid.NewString()
	if _, err = pool.Exec(ctx, "INSERT INTO users(id,user_name,email,password) VALUES($1,$2,$2,'fixture')", user, user); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM users WHERE id=$1", user)
	if _, err = pool.Exec(ctx, `INSERT INTO show_notes(id,owner_id,title,is_live,scene_state) VALUES($1,$2,'private offline fixture',false,'{}')`, note, user); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM show_notes WHERE id=$1", note)
	requestCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("GET", "/api/show-notes/"+note+"/scene/stream", nil).WithContext(requestCtx)
	recorder := httptest.NewRecorder()
	c := echo.New().NewContext(req, recorder)
	c.SetParamNames("id")
	c.SetParamValues(note)
	err = HandleLiveSceneStream(auth.NewSessionManager("audit-only"), dbc, telemetry.NewHub(), producer.NewSceneHub())(c)
	if err == nil && recorder.Code == 200 {
		t.Fatalf("anonymous offline scene returned HTTP 200 and %d bytes of SSE", recorder.Body.Len())
	}
	sm := auth.NewSessionManager("audit-only")
	cookieWriter := httptest.NewRecorder()
	if err := sm.SaveSession(cookieWriter, httptest.NewRequest("GET", "/", nil), user, user, auth.AccessUser); err != nil {
		t.Fatal(err)
	}
	previewCtx, previewCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer previewCancel()
	previewReq := httptest.NewRequest("GET", "/api/show-notes/"+note+"/scene/stream", nil).WithContext(previewCtx)
	for _, cookie := range cookieWriter.Result().Cookies() {
		previewReq.AddCookie(cookie)
	}
	preview := httptest.NewRecorder()
	previewContext := echo.New().NewContext(previewReq, preview)
	previewContext.SetParamNames("id")
	previewContext.SetParamValues(note)
	if err := HandleLiveSceneStream(sm, dbc, telemetry.NewHub(), producer.NewSceneHub())(previewContext); err != nil || preview.Code != 200 || preview.Body.Len() == 0 {
		t.Fatalf("owner preview failed: %v %d", err, preview.Code)
	}
	if _, err := pool.Exec(ctx, "UPDATE show_notes SET is_live=true WHERE id=$1", note); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	e.GET("/api/show-notes/:id/scene/stream", HandleLiveSceneStream(sm, dbc, telemetry.NewHub(), producer.NewSceneHub()))
	server := httptest.NewServer(e)
	defer server.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(server.URL + "/api/show-notes/" + note + "/scene/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("live viewer denied: %d", response.StatusCode)
	}
	if _, err := pool.Exec(ctx, "UPDATE show_notes SET is_live=false WHERE id=$1", note); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("offline stream did not terminate before client timeout: %v", err)
	}
}
