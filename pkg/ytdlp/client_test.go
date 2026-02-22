package ytdlp

import (
	"context"
	"errors"
	"testing"
)

func TestGetInfo_ParsesJSON(t *testing.T) {
	c := New()
	c.execFn = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return []byte(`{"id":"abc","title":"hello","webpage_url":"https://example.com","duration":12}`), nil, nil
	}

	info, err := c.GetInfo(context.Background(), "https://example.com/watch?v=abc")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if info.ID != "abc" {
		t.Fatalf("expected id=abc, got %q", info.ID)
	}
	if info.Title != "hello" {
		t.Fatalf("expected title=hello, got %q", info.Title)
	}
	if len(info.Raw) == 0 {
		t.Fatalf("expected Raw to be set")
	}
}

func TestGetInfo_WrapsExecError(t *testing.T) {
	c := New()
	c.execFn = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return []byte("out"), []byte("err"), errors.New("boom")
	}

	_, err := c.GetInfo(context.Background(), "https://example.com")
	if err == nil {
		t.Fatalf("expected error")
	}
	var ee *ExecError
	if !errors.As(err, &ee) {
		t.Fatalf("expected ExecError, got %T", err)
	}
	if ee.Stderr != "err" {
		t.Fatalf("expected stderr=err, got %q", ee.Stderr)
	}
}

func TestVersion_TrimsOutput(t *testing.T) {
	c := New()
	c.execFn = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return []byte("2025.01.01\n"), nil, nil
	}

	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if v != "2025.01.01" {
		t.Fatalf("expected version to be trimmed, got %q", v)
	}
}
