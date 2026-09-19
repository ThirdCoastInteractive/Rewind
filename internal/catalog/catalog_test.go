package catalog

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

func TestFeeds(t *testing.T) {
	t.Parallel()

	youtube := Feeds(&db.Channel{
		Platform:     "youtube",
		CanonicalURL: "https://www.youtube.com/@example",
	})
	if len(youtube) != 3 {
		t.Fatalf("youtube feeds = %d, want 3", len(youtube))
	}
	want := []Feed{
		{"videos", "https://www.youtube.com/@example/videos"},
		{"shorts", "https://www.youtube.com/@example/shorts"},
		{"streams", "https://www.youtube.com/@example/streams"},
	}
	for i, f := range want {
		if youtube[i] != f {
			t.Errorf("youtube[%d] = %+v, want %+v", i, youtube[i], f)
		}
	}

	stripped := Feeds(&db.Channel{
		Platform:     "youtube",
		CanonicalURL: "https://www.youtube.com/@example/videos/",
	})
	if len(stripped) != 3 || stripped[0].URL != want[0].URL || stripped[1].URL != want[1].URL || stripped[2].URL != want[2].URL {
		t.Fatalf("youtube suffix strip = %+v, want %+v", stripped, want)
	}

	rumble := Feeds(&db.Channel{
		Platform:     "rumble",
		CanonicalURL: "https://rumble.com/c/example/",
	})
	if len(rumble) != 1 || rumble[0] != (Feed{"videos", "https://rumble.com/c/example"}) {
		t.Fatalf("rumble feeds = %+v, want one videos feed", rumble)
	}

	other := Feeds(&db.Channel{
		Platform:     "vimeo",
		CanonicalURL: "https://vimeo.com/user/123",
	})
	if len(other) != 1 || other[0] != (Feed{"videos", "https://vimeo.com/user/123"}) {
		t.Fatalf("other platform feeds = %+v, want one videos feed", other)
	}

	if Feeds(nil) != nil {
		t.Fatal("nil channel should return nil")
	}
	if Feeds(&db.Channel{Platform: "youtube", CanonicalURL: "  "}) != nil {
		t.Fatal("empty canonical URL should return nil")
	}
	if Feeds(&db.Channel{Platform: "twitter", CanonicalURL: "https://x.com/foo"}) != nil {
		t.Fatal("twitter must not expose catalog feeds")
	}
}

func TestSkipPlatform(t *testing.T) {
	t.Parallel()
	if !SkipPlatform("twitter") || !SkipPlatform("x") {
		t.Fatal("twitter/x should be skipped")
	}
	if SkipPlatform("youtube") {
		t.Fatal("youtube should be crawled")
	}
}

func TestIndexChannelEmptyCanonicalURL(t *testing.T) {
	t.Parallel()

	_, err := IndexChannel(context.Background(), nil, &db.Channel{Platform: "youtube"}, pgtype.UUID{}, false)
	if err == nil {
		t.Fatal("expected error for empty canonical URL")
	}
	if !strings.Contains(err.Error(), "canonical feed URL") {
		t.Fatalf("error %q, want canonical feed URL", err)
	}

	_, err = IndexChannel(context.Background(), nil, nil, pgtype.UUID{}, false)
	if err == nil {
		t.Fatal("expected error for nil channel")
	}

	_, err = IndexChannel(context.Background(), nil, &db.Channel{
		Platform:     "twitter",
		CanonicalURL: "https://x.com/foo",
	}, pgtype.UUID{}, false)
	if err == nil || !strings.Contains(err.Error(), "Twitter/X") {
		t.Fatalf("twitter index error = %v", err)
	}
}

func TestPageRange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                string
		next, size, overlap int
		start, end          int
	}{
		{"first page ignores overlap", 1, 100, 10, 1, 100},
		{"later page rewinds overlap", 101, 100, 10, 91, 200},
		{"next below 1 treated as first page", 0, 100, 10, 1, 100},
		{"zero page size uses default", 1, 0, 10, 1, 100},
		{"negative overlap ignored", 101, 100, -5, 101, 200},
		{"overlap clamped to start of playlist", 5, 100, 10, 1, 104},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := PageRange(tc.next, tc.size, tc.overlap)
			if start != tc.start || end != tc.end {
				t.Fatalf("PageRange(%d,%d,%d) = %d,%d want %d,%d",
					tc.next, tc.size, tc.overlap, start, end, tc.start, tc.end)
			}
		})
	}
}
