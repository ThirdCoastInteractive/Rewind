package fileserver

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
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

type memBlob struct {
	urls  map[string]string
	files map[string][]byte
}

func (m *memBlob) Open(_ context.Context, key string) (plugin.Reader, plugin.Stat, error) {
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
	return m.urls[key], nil
}
func (m *memBlob) LocalPath(string) (string, bool) { return "", false }

func TestOpenOrRedirectPrefersExistingObject(t *testing.T) {
	b := &memBlob{
		urls:  map[string]string{"a": "https://cdn.example/a"},
		files: map[string][]byte{"a": []byte("mp4")},
	}
	u, r, name, err := OpenOrRedirect(context.Background(), b, []string{"missing", "a"})
	if r != nil {
		r.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://cdn.example/a" || name != "a" {
		t.Fatalf("url=%s name=%s", u, name)
	}
}

func TestServeKeyLocalDisk(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)
	root := t.TempDir()
	key := "vid/vid.thumbnail.jpg"
	p := filepath.Join(root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin.Use(plugin.Set{Blob: builtin.NewDisk(root)})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := NewFileServer().ServeKey(c, key, "image/jpeg", "private", ETagWeakStat); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestOpenOrRedirectNilBlob(t *testing.T) {
	u, r, _, err := OpenOrRedirect(context.Background(), nil, []string{"a"})
	if r != nil {
		r.Close()
	}
	if u != "" || err != plugin.ErrNotFound {
		t.Fatalf("u=%s err=%v", u, err)
	}
}
