package plugin_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

func TestMasterSourceLocalDisk(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	root := t.TempDir()
	id := "11111111-1111-1111-1111-111111111111"
	masterRel := filepath.Join(id, id+".video.mp4")
	masterPath := filepath.Join(root, masterRel)
	if err := os.MkdirAll(filepath.Dir(masterPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(masterPath, []byte("fake-mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin.Use(plugin.Set{Blob: builtin.NewDisk(root)})

	src, cleanup, err := plugin.MasterSource(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if src != masterPath {
		t.Fatalf("src=%q want %q", src, masterPath)
	}
}

type urlBlob struct {
	urls  map[string]string
	files map[string][]byte
}

type liveMarker struct{}

func (liveMarker) CreateInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (liveMarker) GetInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (liveMarker) ListInputs(context.Context, *plugin.Actor) ([]*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (liveMarker) DeleteInput(context.Context, *plugin.Actor, string) error {
	return plugin.ErrNotSupported
}
func (liveMarker) AddOutput(context.Context, *plugin.Actor, string, string, string) (*plugin.LiveOutput, error) {
	return nil, plugin.ErrNotSupported
}
func (liveMarker) RemoveOutput(context.Context, *plugin.Actor, string, string) error {
	return plugin.ErrNotSupported
}
func (liveMarker) EnableOutput(context.Context, *plugin.Actor, string, string, bool) error {
	return plugin.ErrNotSupported
}
func (liveMarker) Mount(*echo.Echo) {}

func (m *urlBlob) Open(_ context.Context, key string) (plugin.Reader, plugin.Stat, error) {
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
func (m *urlBlob) Create(context.Context, string) (io.WriteCloser, error) {
	return nil, plugin.ErrNotSupported
}
func (m *urlBlob) Remove(context.Context, string) error           { return plugin.ErrNotSupported }
func (m *urlBlob) List(context.Context, string) ([]string, error) { return nil, nil }
func (m *urlBlob) PublicURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return m.urls[key], nil
}
func (m *urlBlob) LocalPath(string) (string, bool) { return "", false }

func TestMasterSourcePublicURL(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	id := "22222222-2222-2222-2222-222222222222"
	key := plugin.VideoKey(id, id+".video.mp4")
	want := "https://cdn.example/" + key
	plugin.Use(plugin.Set{Blob: &urlBlob{
		urls:  map[string]string{key: want},
		files: map[string][]byte{key: []byte("remote-mp4")},
	}})

	src, cleanup, err := plugin.MasterSource(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if src != want {
		t.Fatalf("src=%q want %q", src, want)
	}
}

func TestMasterSourceDoesNotPresignMissingPreferredExtension(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	id := "55555555-5555-5555-5555-555555555555"
	mp4Key := plugin.VideoKey(id, id+".video.mp4")
	webmKey := plugin.VideoKey(id, id+".video.webm")
	webmURL := "https://cdn.example/" + webmKey
	plugin.Use(plugin.Set{Blob: &urlBlob{
		// A signer can produce a URL for a missing object. Open must establish
		// which extension actually exists before accepting that URL.
		urls:  map[string]string{mp4Key: "https://cdn.example/missing.mp4", webmKey: webmURL},
		files: map[string][]byte{webmKey: []byte("remote-webm")},
	}})

	src, cleanup, err := plugin.MasterSource(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if src != webmURL {
		t.Fatalf("src=%q want %q", src, webmURL)
	}
}

func TestMasterSourceOpenTemp(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	id := "33333333-3333-3333-3333-333333333333"
	key := plugin.VideoKey(id, id+".video.webm")
	plugin.Use(plugin.Set{Blob: &urlBlob{
		files: map[string][]byte{key: []byte("remote-webm")},
	}})

	src, cleanup, err := plugin.MasterSource(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !strings.Contains(filepath.Base(src), "rewind-master-") {
		t.Fatalf("temp name %q", src)
	}
	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "remote-webm" {
		t.Fatalf("content %q", got)
	}
	cleanup()
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("cleanup left file: %v", err)
	}
}

func TestMasterSourceMissing(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	plugin.Use(plugin.Set{Blob: &urlBlob{}})
	_, cleanup, err := plugin.MasterSource(context.Background(), "44444444-4444-4444-4444-444444444444")
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMasterSourceAtScopedPrivateKey(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	tenant := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	key := "org/" + tenant + "/acceptance/master.mp4"
	want := "https://cdn.example/" + key
	plugin.Use(plugin.Set{Blob: &urlBlob{urls: map[string]string{key: want}}})

	ctx := plugin.WithTenantScope(context.Background(), tenant, true)
	src, cleanup, err := plugin.MasterSourceAt(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if src != want {
		t.Fatalf("src=%q want %q", src, want)
	}
}

func TestMasterSourceAtRejectsCrossWorkspaceAndURL(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	tenant := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	plugin.Use(plugin.Set{Blob: &urlBlob{urls: map[string]string{}}})
	ctx := plugin.WithTenantScope(context.Background(), tenant, true)
	for _, key := range []string{
		"org/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb/foreign/master.mp4",
		"https://evil.example/master.mp4",
		"../master.mp4",
	} {
		if src, cleanup, err := plugin.MasterSourceAt(ctx, key); err == nil {
			cleanup()
			t.Fatalf("key %q unexpectedly resolved to %q", key, src)
		}
	}
}

func TestMasterSourceAtLiveRequiresScope(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)
	plugin.Use(plugin.Set{Live: liveMarker{}, Blob: &urlBlob{}})
	if _, cleanup, err := plugin.MasterSourceAt(context.Background(), "org/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/master.mp4"); err == nil {
		cleanup()
		t.Fatal("expected missing Live tenant scope error")
	}
}
