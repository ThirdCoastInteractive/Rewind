//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/stitch_api"
	"thirdcoast.systems/rewind/internal/stitch"
)

func TestStitchHTTPCommandsConflictRetryAndOwnership(t *testing.T) {
	d, owner, project := openStitchTest(t)
	_, other, _ := openStitchTest(t)
	ctx := context.Background()
	store := stitch.NewStore(d)
	initial, err := store.Enable(ctx, owner, project)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager("disposable-stitch-http-fixture")
	e := echo.New()
	stitch_api.RegisterEditor(e.Group("/api"), sm, d)
	stitch_api.RegisterEditorMedia(e.Group("/api"), sm, d, t.TempDir())
	base := "/api/stitch/projects/" + project.String()
	request := func(user, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if user != "" {
			cookies := httptest.NewRecorder()
			if err := sm.SaveSession(cookies, req, user, user, auth.AccessUser); err != nil {
				t.Fatal(err)
			}
			for _, cookie := range cookies.Result().Cookies() {
				req.AddCookie(cookie)
			}
		}
		res := httptest.NewRecorder()
		e.ServeHTTP(res, req)
		return res
	}
	body := map[string]any{"expected_revision": initial.Revision, "operation_key": "http-first", "summary": "Human title edit", "operations": []stitch.Operation{{Type: "set_title", Title: "HTTP title"}}}
	first := request(owner.String(), "POST", base+"/commands", body)
	if first.Code != http.StatusOK {
		t.Fatalf("commit: %d %s", first.Code, first.Body.String())
	}
	var result struct {
		Revision int64           `json:"revision"`
		Summary  string          `json:"summary"`
		Document stitch.Document `json:"document"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Revision != initial.Revision+1 || result.Summary != "Human title edit" || result.Document.Title != "HTTP title" {
		t.Fatalf("commit response: %+v", result)
	}
	retry := request(owner.String(), "POST", base+"/commands", body)
	if retry.Code != 200 || !bytes.Equal(first.Body.Bytes(), retry.Body.Bytes()) {
		t.Fatalf("retry changed original response: %d %s", retry.Code, retry.Body.String())
	}
	body["operation_key"] = "http-stale"
	stale := request(owner.String(), "POST", base+"/commands", body)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale: %d %s", stale.Code, stale.Body.String())
	}
	// Projects are owner-only: another user's read looks the same as a missing
	// project, like their writes.
	for _, path := range []string{"/document", "/history"} {
		for _, user := range []string{other.String(), ""} {
			res := request(user, "GET", base+path, nil)
			if user == "" {
				if res.Code != http.StatusUnauthorized && res.Code != http.StatusNotFound {
					t.Fatalf("anonymous read %s: %d %s", path, res.Code, res.Body.String())
				}
			} else if res.Code != http.StatusNotFound {
				t.Fatalf("foreign read %s: %d %s", path, res.Code, res.Body.String())
			}
		}
	}
	body["expected_revision"] = result.Revision
	if res := request(other.String(), "POST", base+"/commands", body); res.Code != http.StatusNotFound {
		t.Fatalf("foreign write: %d %s", res.Code, res.Body.String())
	}
	snap, err := store.Get(ctx, owner, project)
	if err != nil || snap.Revision != result.Revision || snap.Document.Title != result.Document.Title {
		t.Fatalf("rejected requests changed state: %+v %v", snap, err)
	}
	undo := request(owner.String(), "POST", base+"/undo", map[string]any{"expected_revision": result.Revision, "operation_key": "http-undo"})
	if undo.Code != 200 {
		t.Fatalf("undo: %d %s", undo.Code, undo.Body.String())
	}
	snap, err = store.Get(ctx, owner, project)
	if err != nil || snap.Document.Title != initial.Document.Title || snap.Revision != result.Revision+1 {
		t.Fatalf("undo state: %+v %v", snap, err)
	}
	history := request(owner.String(), "GET", base+"/history", nil)
	if history.Code != 200 || !bytes.Contains(history.Body.Bytes(), []byte("Human title edit")) {
		t.Fatalf("history: %d %s", history.Code, history.Body.String())
	}
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	asset, err := store.AddAsset(ctx, owner, project, bytes.NewReader(pixels.Bytes()), "image/png", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	assetPath := base + "/assets/" + asset.ID.String()
	if res := request(owner.String(), "GET", assetPath, nil); res.Code != 200 {
		t.Fatalf("asset read: %d %s", res.Code, res.Body.String())
	}
	if res := request(other.String(), "GET", assetPath, nil); res.Code != 200 {
		t.Fatalf("shared asset read: %d", res.Code)
	}
	seed, err := store.Commit(ctx, owner, project, snap.Revision, "render-fixture", stitch.Actor{Kind: "user", ID: owner.String()}, "Add render fixture", []stitch.Operation{{Type: "insert_segment", Segment: &stitch.Segment{Type: "title", DurationUS: 1_000_000}}})
	if err != nil {
		t.Fatal(err)
	}
	renderBody := map[string]any{"revision": seed.Revision, "operation_key": "frame-zero", "frame_time_us": 0, "format": "mp4", "quality": "high", "caption_mode": "none", "scope": "all"}
	frame := request(owner.String(), "POST", base+"/frames", renderBody)
	if frame.Code != 202 && frame.Code != 200 {
		t.Fatalf("frame at zero: %d %s", frame.Code, frame.Body.String())
	}
	var job struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal(frame.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Revision != seed.Revision || job.ID == "" {
		t.Fatalf("frame response: %+v", job)
	}
	for _, path := range []string{"/frames", "/previews", "/exports"} {
		renderBody["start_us"], renderBody["end_us"] = 0, 1_000_000
		renderBody["scope"] = "range"
		if res := request(other.String(), "POST", base+path, renderBody); res.Code != 404 {
			t.Fatalf("foreign render %s: %d %s", path, res.Code, res.Body.String())
		}
	}
	statusPath := base + "/render-jobs/" + job.ID
	if res := request(other.String(), "GET", statusPath, nil); res.Code != 404 {
		t.Fatalf("foreign render status: %d %s", res.Code, res.Body.String())
	}
	listed := request(owner.String(), "GET", base+"/render-jobs", nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("render job list: %d %s", listed.Code, listed.Body.String())
	}
	var jobs struct {
		Jobs []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs.Jobs) == 0 || jobs.Jobs[0].ID == "" {
		t.Fatalf("render job list empty: %s", listed.Body.String())
	}
	foreignList := request(other.String(), "GET", base+"/render-jobs", nil)
	if foreignList.Code != http.StatusOK {
		t.Fatalf("foreign render list: %d %s", foreignList.Code, foreignList.Body.String())
	}
	var leaked struct {
		Jobs []struct {
			ID string `json:"id"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(foreignList.Body.Bytes(), &leaked); err != nil {
		t.Fatal(err)
	}
	if len(leaked.Jobs) != 0 {
		t.Fatalf("foreign list leaked jobs: %s", foreignList.Body.String())
	}
	if _, err := d.Exec(ctx, "UPDATE users SET enabled=false WHERE id=$1", owner); err != nil {
		t.Fatal(err)
	}
	if res := request(owner.String(), "GET", base+"/document", nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("disabled account: %d", res.Code)
	}
}

func TestStitchHTTPYouTube(t *testing.T) {
	d, owner, project := openStitchTest(t)
	_, other, _ := openStitchTest(t)
	ctx := context.Background()
	store := stitch.NewStore(d)
	initial, err := store.Enable(ctx, owner, project)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager("disposable-stitch-youtube-fixture")
	e := echo.New()
	stitch_api.RegisterEditor(e.Group("/api"), sm, d)
	base := "/api/stitch/projects/" + project.String()
	request := func(user, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if user != "" {
			cookies := httptest.NewRecorder()
			if err := sm.SaveSession(cookies, req, user, user, auth.AccessUser); err != nil {
				t.Fatal(err)
			}
			for _, cookie := range cookies.Result().Cookies() {
				req.AddCookie(cookie)
			}
		}
		res := httptest.NewRecorder()
		e.ServeHTTP(res, req)
		return res
	}
	body := map[string]any{"description": "Hello YouTube", "tags": []string{"clips", "shorts"}}
	put := request(owner.String(), "PUT", base+"/youtube", body)
	if put.Code != http.StatusOK {
		t.Fatalf("youtube put: %d %s", put.Code, put.Body.String())
	}
	var putSnap struct {
		Revision    int64    `json:"revision"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	}
	if err := json.Unmarshal(put.Body.Bytes(), &putSnap); err != nil {
		t.Fatal(err)
	}
	if putSnap.Revision != initial.Revision {
		t.Fatalf("youtube put bumped revision: got %d want %d", putSnap.Revision, initial.Revision)
	}
	if putSnap.Description != "Hello YouTube" {
		t.Fatalf("description: %q", putSnap.Description)
	}
	if len(putSnap.Tags) != 2 || putSnap.Tags[0] != "clips" || putSnap.Tags[1] != "shorts" {
		t.Fatalf("tags: %#v", putSnap.Tags)
	}
	doc := request(owner.String(), "GET", base+"/document", nil)
	if doc.Code != http.StatusOK {
		t.Fatalf("document: %d %s", doc.Code, doc.Body.String())
	}
	var got struct {
		Revision    int64    `json:"revision"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	}
	if err := json.Unmarshal(doc.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Revision != initial.Revision || got.Description != "Hello YouTube" || len(got.Tags) != 2 {
		t.Fatalf("document youtube fields: %+v", got)
	}
	if res := request(other.String(), "PUT", base+"/youtube", body); res.Code != http.StatusUnauthorized && res.Code != http.StatusNotFound {
		t.Fatalf("foreign youtube put: %d %s", res.Code, res.Body.String())
	}
	if res := request("", "PUT", base+"/youtube", body); res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated youtube put: %d %s", res.Code, res.Body.String())
	}
	long := map[string]any{"description": strings.Repeat("x", 5001), "tags": []string{}}
	if res := request(owner.String(), "PUT", base+"/youtube", long); res.Code != http.StatusBadRequest {
		t.Fatalf("over-long description: %d %s", res.Code, res.Body.String())
	}
	snap, err := store.Get(ctx, owner, project)
	if err != nil || snap.Revision != initial.Revision || snap.Description != "Hello YouTube" {
		t.Fatalf("rejected over-long changed state: %+v %v", snap, err)
	}
}
