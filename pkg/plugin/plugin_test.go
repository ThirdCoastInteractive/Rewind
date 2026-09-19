package plugin_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

func TestDiskRoundTrip(t *testing.T) {
	root := t.TempDir()
	d := builtin.NewDisk(root)
	w, err := d.Create(t.Context(), "vid/master.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, st, err := d.Open(t.Context(), "vid/master.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if st.Size != 5 {
		r.Close()
		t.Fatalf("size %d", st.Size)
	}
	buf := make([]byte, 5)
	if _, err := r.Read(buf); err != nil {
		r.Close()
		t.Fatal(err)
	}
	if string(buf) != "hello" {
		r.Close()
		t.Fatalf("got %q", buf)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	p, ok := d.LocalPath("vid/master.mp4")
	if !ok {
		t.Fatal("expected local path")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != filepath.Join(root, "vid") {
		t.Fatalf("dir %s", p)
	}

	keys, err := d.List(t.Context(), "vid")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "vid/master.mp4" {
		t.Fatalf("list %v", keys)
	}

	if err := d.Remove(t.Context(), "vid/master.mp4"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Open(t.Context(), "vid/master.mp4"); err != plugin.ErrNotFound {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestUseMerges(t *testing.T) {
	plugin.Use(plugin.Set{Blob: builtin.NewDisk(t.TempDir())})
	if plugin.Blobs() == nil {
		t.Fatal("blob")
	}
}

func TestVideoKey(t *testing.T) {
	if plugin.VideoKey("abc", "a.mp4") != "abc/a.mp4" {
		t.Fatal(plugin.VideoKey("abc", "a.mp4"))
	}
}

type fakeML struct{ jobs []plugin.Job }

func (f *fakeML) Enqueue(_ context.Context, job plugin.Job) (string, error) {
	f.jobs = append(f.jobs, job)
	return "jid", nil
}

func TestEnqueueGoesThroughPlugin(t *testing.T) {
	f := &fakeML{}
	plugin.Use(plugin.Set{ML: f})
	if err := plugin.Transcribe(context.Background(), "11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatal(err)
	}
	if len(f.jobs) != 1 || f.jobs[0].Kind != plugin.KindTranscribe {
		t.Fatalf("%+v", f.jobs)
	}
}
