package plugin_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"thirdcoast.systems/rewind/pkg/plugin"
)

type memBlob struct {
	keys map[string]struct{}
}

func (m *memBlob) Open(context.Context, string) (plugin.Reader, plugin.Stat, error) {
	return nil, plugin.Stat{}, plugin.ErrNotFound
}
func (m *memBlob) Create(context.Context, string) (io.WriteCloser, error) { return nil, nil }
func (m *memBlob) Remove(_ context.Context, key string) error {
	delete(m.keys, key)
	return nil
}
func (m *memBlob) List(_ context.Context, prefix string) ([]string, error) {
	var out []string
	for k := range m.keys {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}
func (m *memBlob) PublicURL(context.Context, string, time.Duration) (string, error) { return "", nil }
func (m *memBlob) LocalPath(string) (string, bool)                                 { return "", false }

func TestRemoveVideoBlobsDeletesPrefixAndMaster(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)
	b := &memBlob{keys: map[string]struct{}{
		"vid/thumb.jpg":            {},
		"vid/seek/seek.json":       {},
		"stream/stream.video.mp4":  {},
		"other/keep.mp4":           {},
	}}
	plugin.Use(plugin.Set{Blob: b})
	plugin.RemoveVideoBlobs(context.Background(), "vid", "stream/stream.video.mp4")
	if _, ok := b.keys["other/keep.mp4"]; !ok {
		t.Fatal("unrelated key removed")
	}
	if len(b.keys) != 1 {
		t.Fatalf("leftover %#v", b.keys)
	}
}
