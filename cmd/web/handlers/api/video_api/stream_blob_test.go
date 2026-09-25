package video_api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

type memBlob struct {
	urls    map[string]string
	files   map[string][]byte
	opens   []string
	publics []string
}

func (m *memBlob) Open(_ context.Context, key string) (plugin.Reader, plugin.Stat, error) {
	m.opens = append(m.opens, key)
	b, ok := m.files[key]
	if !ok {
		return nil, plugin.Stat{}, plugin.ErrNotFound
	}
	r := bytes.NewReader(b)
	return struct {
		io.ReadCloser
		io.Seeker
	}{io.NopCloser(r), r}, plugin.Stat{Size: int64(len(b))}, nil
}

func (m *memBlob) Create(context.Context, string) (io.WriteCloser, error) {
	return nil, plugin.ErrNotSupported
}
func (m *memBlob) Remove(context.Context, string) error           { return plugin.ErrNotSupported }
func (m *memBlob) List(context.Context, string) ([]string, error) { return nil, nil }
func (m *memBlob) PublicURL(_ context.Context, key string, _ time.Duration) (string, error) {
	m.publics = append(m.publics, key)
	return m.urls[key], nil
}
func (m *memBlob) LocalPath(string) (string, bool) { return "", false }

func TestBlobKeyKeepsLeadingSlashPrefix(t *testing.T) {
	got := blobKey("/tenant/uuid/file.mp4", "uuid")
	if got != "/tenant/uuid/file.mp4" {
		t.Fatalf("got %q", got)
	}
}

func TestStreamKeysPrefersStoredKey(t *testing.T) {
	keys := streamKeys("vid", "/tenant/uuid/file.mp4")
	if len(keys) == 0 || keys[0] != "/tenant/uuid/file.mp4" {
		t.Fatalf("%v", keys)
	}
}

func TestStreamKeysAddsExistingLocalKeyForLegacyAbsolutePath(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)
	root := t.TempDir()
	videoID := "11111111-1111-1111-1111-111111111111"
	name := videoID + ".video.mov"
	path := filepath.Join(root, videoID, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin.Use(plugin.Set{Blob: builtin.NewDisk(root)})

	keys := streamKeys(videoID, path)
	want := plugin.VideoKey(videoID, name)
	for _, key := range keys {
		if key == want {
			return
		}
	}
	t.Fatalf("keys %v missing existing local key %q", keys, want)
}

func TestOpenOrRedirectMissingPublicURLDoesNotRedirect(t *testing.T) {
	b := &memBlob{
		urls:  map[string]string{"/tenant/uuid/file.mp4": "https://cdn.example/missing"},
		files: map[string][]byte{},
	}
	u, r, _, err := fileserver.OpenOrRedirect(context.Background(), b, []string{"/tenant/uuid/file.mp4"})
	if r != nil {
		r.Close()
	}
	if u != "" {
		t.Fatalf("redirected to %s for missing object", u)
	}
	if err != plugin.ErrNotFound {
		t.Fatalf("err %v", err)
	}
}

func TestOpenOrRedirectOpensStoredKey(t *testing.T) {
	key := "/tenant/uuid/file.mp4"
	b := &memBlob{
		urls:  map[string]string{},
		files: map[string][]byte{key: []byte("mp4")},
	}
	u, r, name, err := fileserver.OpenOrRedirect(context.Background(), b, []string{key})
	if err != nil {
		t.Fatal(err)
	}
	if u != "" {
		t.Fatalf("unexpected redirect %s", u)
	}
	if name != key {
		t.Fatalf("opened %s", name)
	}
	if r == nil {
		t.Fatal("nil reader")
	}
	r.Close()
	if len(b.opens) != 1 || b.opens[0] != key {
		t.Fatalf("opens %v", b.opens)
	}
}

type tenantAuthn struct {
	tenant string
}

func (t tenantAuthn) Current(*http.Request) (*plugin.Actor, error) {
	return &plugin.Actor{UserID: "user-1", Name: "t", TenantID: t.tenant, Roles: []string{"user"}}, nil
}
func (tenantAuthn) Login(http.ResponseWriter, *http.Request) error  { return plugin.ErrNotSupported }
func (tenantAuthn) Logout(http.ResponseWriter, *http.Request) error { return nil }
func (tenantAuthn) Register(http.ResponseWriter, *http.Request) error {
	return plugin.ErrNotSupported
}
func (tenantAuthn) LoginPath() string { return "/login" }
func (tenantAuthn) Mount(*echo.Echo)  {}

func TestHandleStreamTenantMissDoesNotOpenBlobOrDisk(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	otherID := "22222222-2222-2222-2222-222222222222"
	blob := &memBlob{
		urls:  map[string]string{plugin.VideoKey(otherID, otherID+".video.mp4"): "https://cdn.example/stolen"},
		files: map[string][]byte{plugin.VideoKey(otherID, otherID+".video.mp4"): []byte("secret")},
	}
	plugin.Use(plugin.Set{
		Authn: tenantAuthn{tenant: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"},
		Blob:  blob,
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+otherID+"/stream", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/videos/:id/stream")
	c.SetParamNames("id")
	c.SetParamValues(otherID)

	sm := auth.NewSessionManager("test-secret")
	if err := HandleStream(sm, nil)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 404 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Location") != "" {
		t.Fatalf("redirected %s", rec.Header().Get("Location"))
	}
	if len(blob.opens) != 0 || len(blob.publics) != 0 {
		t.Fatalf("blob touched opens=%v publics=%v", blob.opens, blob.publics)
	}
}

func TestOpenOrRedirectAfterOpen(t *testing.T) {
	key := "/tenant/uuid/file.mp4"
	b := &memBlob{
		urls:  map[string]string{key: "https://cdn.example/ok"},
		files: map[string][]byte{key: []byte("mp4")},
	}
	u, r, _, err := fileserver.OpenOrRedirect(context.Background(), b, []string{key})
	if r != nil {
		r.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://cdn.example/ok" {
		t.Fatalf("url %s", u)
	}
}
