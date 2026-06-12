package ytdlp

import (
	"context"
	"errors"
	"testing"
)

func TestListPlaylistEntries_ParsesEntries(t *testing.T) {
	c := New()
	c.execFn = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return []byte(`{
			"id": "PL123",
			"title": "My Playlist",
			"entries": [
				{"id": "aaa", "url": "https://www.youtube.com/watch?v=aaa", "title": "First"},
				{"id": "bbb", "url": "bbb", "title": "Second"},
				{"id": "", "url": "ccc", "title": "Skipped"}
			]
		}`), nil, nil
	}

	entries, err := c.ListPlaylistEntries(context.Background(), "https://www.youtube.com/playlist?list=PL123")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (empty id skipped), got %d: %+v", len(entries), entries)
	}
	if entries[0].ID != "aaa" || entries[0].Title != "First" || entries[0].URL != "https://www.youtube.com/watch?v=aaa" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
	if entries[1].ID != "bbb" || entries[1].URL != "bbb" {
		t.Fatalf("unexpected second entry: %+v", entries[1])
	}
}

func TestListPlaylistEntries_SingleVideo(t *testing.T) {
	c := New()
	c.execFn = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		// Non-playlist URL: yt-dlp emits a single video object with no entries.
		return []byte(`{"id": "abc", "url": "https://www.youtube.com/watch?v=abc", "title": "Solo"}`), nil, nil
	}

	entries, err := c.ListPlaylistEntries(context.Background(), "https://www.youtube.com/watch?v=abc")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(entries), entries)
	}
	if entries[0].ID != "abc" || entries[0].Title != "Solo" {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}
}

func TestListPlaylistEntries_SingleVideoNoID(t *testing.T) {
	c := New()
	c.execFn = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return []byte(`{"title": "No ID here"}`), nil, nil
	}

	entries, err := c.ListPlaylistEntries(context.Background(), "https://example.com/thing")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d: %+v", len(entries), entries)
	}
}

func TestListPlaylistEntries_EmptyURL(t *testing.T) {
	c := New()
	_, err := c.ListPlaylistEntries(context.Background(), "  ")
	if err == nil {
		t.Fatalf("expected error for empty url")
	}
}

func TestListPlaylistEntries_WrapsExecError(t *testing.T) {
	c := New()
	c.execFn = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return []byte("out"), []byte("err"), errors.New("boom")
	}

	_, err := c.ListPlaylistEntries(context.Background(), "https://www.youtube.com/playlist?list=PL123")
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
