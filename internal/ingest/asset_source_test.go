package ingest

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/pkg/plugin"
)

type liveAssetMarker struct{}

func (liveAssetMarker) CreateInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (liveAssetMarker) GetInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (liveAssetMarker) ListInputs(context.Context, *plugin.Actor) ([]*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (liveAssetMarker) DeleteInput(context.Context, *plugin.Actor, string) error {
	return plugin.ErrNotSupported
}
func (liveAssetMarker) AddOutput(context.Context, *plugin.Actor, string, string, string) (*plugin.LiveOutput, error) {
	return nil, plugin.ErrNotSupported
}
func (liveAssetMarker) RemoveOutput(context.Context, *plugin.Actor, string, string) error {
	return plugin.ErrNotSupported
}
func (liveAssetMarker) EnableOutput(context.Context, *plugin.Actor, string, string, bool) error {
	return plugin.ErrNotSupported
}
func (liveAssetMarker) Mount(*echo.Echo) {}

type assetSourceBlob struct {
	urls  map[string]string
	files map[string][]byte
}

func (b *assetSourceBlob) Open(_ context.Context, key string) (plugin.Reader, plugin.Stat, error) {
	data, ok := b.files[key]
	if !ok {
		return nil, plugin.Stat{}, plugin.ErrNotFound
	}
	r := bytes.NewReader(data)
	return struct {
		io.ReadCloser
		io.Seeker
	}{io.NopCloser(r), r}, plugin.Stat{Size: int64(len(data))}, nil
}

func (*assetSourceBlob) Create(context.Context, string) (io.WriteCloser, error) {
	return nil, plugin.ErrNotSupported
}
func (*assetSourceBlob) Remove(context.Context, string) error           { return plugin.ErrNotSupported }
func (*assetSourceBlob) List(context.Context, string) ([]string, error) { return nil, nil }
func (b *assetSourceBlob) PublicURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return b.urls[key], nil
}
func (*assetSourceBlob) LocalPath(string) (string, bool) { return "", false }

func TestPrepareAssetWorkUsesImportedBlobKey(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)
	spool := t.TempDir()
	t.Setenv("SPOOL_DIR", spool)

	videoID := "11111111-1111-1111-1111-111111111111"
	key := "tenant/live-recording/master.mkv"
	wantURL := "https://cdn.example/" + key
	plugin.Use(plugin.Set{Blob: &assetSourceBlob{urls: map[string]string{key: wantURL}}})

	work, err := prepareAssetWork(context.Background(), videoID, key)
	if err != nil {
		t.Fatal(err)
	}
	if work.Src != wantURL {
		t.Fatalf("source=%q want %q", work.Src, wantURL)
	}
	if work.Dir == "" {
		t.Fatal("expected temporary asset output directory")
	}
	work.Cleanup()
	if _, err := os.Stat(work.Dir); !os.IsNotExist(err) {
		t.Fatalf("asset output directory still exists: %v", err)
	}
}

func TestPrepareAssetWorkSpoolsImportedBlobKey(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)
	spool := t.TempDir()
	t.Setenv("SPOOL_DIR", spool)

	videoID := "22222222-2222-2222-2222-222222222222"
	key := "tenant/live-recording/master.webm"
	plugin.Use(plugin.Set{Blob: &assetSourceBlob{files: map[string][]byte{key: []byte("webm")}}})

	work, err := prepareAssetWork(context.Background(), videoID, key)
	if err != nil {
		t.Fatal(err)
	}
	defer work.Cleanup()
	if work.Src == key || work.Src == "" {
		t.Fatalf("source=%q should be a local spool file", work.Src)
	}
	data, err := os.ReadFile(work.Src)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "webm" {
		t.Fatalf("spooled content=%q", data)
	}
}

func TestResolveLiveAssetRejectsLocalPathAndAcceptsTenantKey(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)
	tenant := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	localPath := t.TempDir() + "/master.mp4"
	if err := os.WriteFile(localPath, []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := "org/" + tenant + "/acceptance/master.mp4"
	wantURL := "https://cdn.example/" + key
	plugin.Use(plugin.Set{Live: liveAssetMarker{}, Blob: &assetSourceBlob{urls: map[string]string{key: wantURL}}})
	ctx := plugin.WithTenantScope(context.Background(), tenant, true)
	if _, _, err := resolveAssetSource(ctx, "11111111-1111-1111-1111-111111111111", localPath); err == nil {
		t.Fatal("expected local path rejection for Live source")
	}
	src, cleanup, err := resolveAssetSource(ctx, "11111111-1111-1111-1111-111111111111", key)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if src != wantURL {
		t.Fatalf("source=%q want %q", src, wantURL)
	}
}

func TestIsPrivateMasterKey(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"org/tenant/master.mp4", true},
		{"/spool/downloads/video.mp4", false},
		{"tenant/master.mp4", false},
	} {
		if got := isPrivateMasterKey(tc.path); got != tc.want {
			t.Fatalf("isPrivateMasterKey(%q)=%v want %v", tc.path, got, tc.want)
		}
	}
}

func TestOmitRemoteSourceStatusDoesNotClaimLocalSuccess(t *testing.T) {
	status := map[string]any{
		"video_file": true, "file_hash": true, "faststart": true,
		"captions_clean": true, "preview": false,
	}
	omitRemoteSourceStatus(status)
	for _, key := range []string{"video_file", "file_hash", "faststart", "captions_clean"} {
		if _, ok := status[key]; ok {
			t.Fatalf("remote status retained inapplicable %q", key)
		}
	}
	if got := status["preview"]; got != false {
		t.Fatalf("derived preview status changed: %v", got)
	}
}
