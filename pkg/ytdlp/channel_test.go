package ytdlp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// channelMock returns an execFn that serves canned JSON keyed by the URL
// argument (always the last arg) and records each invocation's args.
func channelMock(t *testing.T, responses map[string]string, calls *[][]string) func(context.Context, string, ...string) ([]byte, []byte, error) {
	t.Helper()
	return func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		if calls != nil {
			*calls = append(*calls, args)
		}
		url := args[len(args)-1]
		body, ok := responses[url]
		if !ok {
			return nil, []byte("no such url"), errors.New("unexpected url: " + url)
		}
		return []byte(body), nil, nil
	}
}

func TestListChannelVideos_ExpandsYouTubeTabs(t *testing.T) {
	var calls [][]string
	c := New()
	c.execFn = channelMock(t, map[string]string{
		"https://www.youtube.com/@somechannel": `{
			"id": "UC123", "title": "Some Channel",
			"entries": [
				{"id": "UC123-videos", "url": "https://www.youtube.com/@somechannel/videos", "title": "Some Channel - Videos", "_type": "url", "ie_key": "YoutubeTab"},
				{"id": "UC123-shorts", "url": "https://www.youtube.com/@somechannel/shorts", "title": "Some Channel - Shorts", "_type": "url", "ie_key": "YoutubeTab"}
			]
		}`,
		"https://www.youtube.com/@somechannel/videos": `{
			"id": "UC123-videos", "title": "Some Channel - Videos",
			"entries": [
				{"id": "vid1", "url": "https://www.youtube.com/watch?v=vid1", "title": "One", "_type": "url", "ie_key": "Youtube"},
				{"id": "vid2", "url": "https://www.youtube.com/watch?v=vid2", "title": "Two", "_type": "url", "ie_key": "Youtube"}
			]
		}`,
		"https://www.youtube.com/@somechannel/shorts": `{
			"id": "UC123-shorts", "title": "Some Channel - Shorts",
			"entries": [
				{"id": "short1", "url": "https://www.youtube.com/watch?v=short1", "title": "Short", "_type": "url", "ie_key": "Youtube"},
				{"id": "vid2", "url": "https://www.youtube.com/watch?v=vid2", "title": "Two (dup)", "_type": "url", "ie_key": "Youtube"}
			]
		}`,
	}, &calls)

	listing, err := c.ListChannelVideos(context.Background(), "https://www.youtube.com/@somechannel", 50)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if listing.Title != "Some Channel" {
		t.Fatalf("expected wrapper title, got %q", listing.Title)
	}
	// vid1, vid2, short1 — vid2 deduped across tabs, tab entries never returned.
	if len(listing.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(listing.Entries), listing.Entries)
	}
	ids := map[string]bool{}
	for _, e := range listing.Entries {
		ids[e.ID] = true
	}
	if !ids["vid1"] || !ids["vid2"] || !ids["short1"] {
		t.Fatalf("unexpected entry ids: %+v", ids)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 yt-dlp invocations (wrapper + 2 tabs), got %d", len(calls))
	}
	// The head limit must be applied to every enumeration.
	for _, args := range calls {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--playlist-end 50") {
			t.Fatalf("expected --playlist-end 50 in args: %v", args)
		}
	}
}

func TestListChannelVideos_InlineNestedTabs(t *testing.T) {
	// Current yt-dlp resolves bare-channel tabs inline: the wrapper's entries
	// are playlist objects that carry their own nested "entries" array (and no
	// per-tab URL). Everything must flatten from the single invocation.
	var calls [][]string
	c := New()
	c.execFn = channelMock(t, map[string]string{
		"https://www.youtube.com/@inline": `{
			"id": "@inline", "title": "Inline Channel", "_type": "playlist",
			"entries": [
				{
					"id": "UCinline", "title": "Inline Channel - Videos", "_type": "playlist",
					"entries": [
						{"id": "n1", "url": "https://www.youtube.com/watch?v=n1", "title": "Nested One", "_type": "url", "ie_key": "Youtube"},
						{"id": "n2", "url": "https://www.youtube.com/watch?v=n2", "title": "Nested Two", "_type": "url", "ie_key": "Youtube"}
					]
				},
				{
					"id": "UCinline-live", "title": "Inline Channel - Live", "_type": "playlist",
					"entries": []
				}
			]
		}`,
	}, &calls)

	listing, err := c.ListChannelVideos(context.Background(), "https://www.youtube.com/@inline", 100)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("inline-nested tabs must not trigger extra invocations, got %d", len(calls))
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("expected 2 flattened entries, got %d: %+v", len(listing.Entries), listing.Entries)
	}
	if listing.Entries[0].ID != "n1" || listing.Entries[1].ID != "n2" {
		t.Fatalf("unexpected entries: %+v", listing.Entries)
	}
}

func TestListPlaylistEntries_FlattensInlineNestedTabs(t *testing.T) {
	// The plain playlist path gets the same treatment: a bare channel URL must
	// yield leaf videos, never tab playlist objects masquerading as videos.
	c := New()
	c.execFn = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return []byte(`{
			"id": "@x", "title": "X", "_type": "playlist",
			"entries": [
				{"id": "UCx", "title": "X - Videos", "_type": "playlist",
				 "entries": [{"id": "v1", "url": "https://www.youtube.com/watch?v=v1", "title": "V1", "ie_key": "Youtube"}]}
			]
		}`), nil, nil
	}

	entries, err := c.ListPlaylistEntries(context.Background(), "https://www.youtube.com/@x")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "v1" {
		t.Fatalf("expected only the leaf video, got %+v", entries)
	}
}

func TestListChannelVideos_FlatChannelNoRecursion(t *testing.T) {
	var calls [][]string
	c := New()
	c.execFn = channelMock(t, map[string]string{
		"https://rumble.com/c/somebody": `{
			"id": "c-somebody", "title": "Somebody",
			"entries": [
				{"id": "r1", "url": "https://rumble.com/v1.html", "title": "First", "_type": "url"},
				{"id": "r2", "url": "https://rumble.com/v2.html", "title": "Second", "_type": "url"}
			]
		}`,
	}, &calls)

	listing, err := c.ListChannelVideos(context.Background(), "https://rumble.com/c/somebody", 100)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(listing.Entries), listing.Entries)
	}
	if len(calls) != 1 {
		t.Fatalf("expected a single invocation for a flat channel, got %d", len(calls))
	}
}

func TestListChannelVideos_TabFailureIsSkipped(t *testing.T) {
	c := New()
	c.execFn = channelMock(t, map[string]string{
		"https://www.youtube.com/@x": `{
			"id": "UCx", "title": "X",
			"entries": [
				{"id": "UCx-videos", "url": "https://www.youtube.com/@x/videos", "_type": "url", "ie_key": "YoutubeTab"},
				{"id": "UCx-streams", "url": "https://www.youtube.com/@x/streams", "_type": "url", "ie_key": "YoutubeTab"}
			]
		}`,
		"https://www.youtube.com/@x/videos": `{
			"id": "UCx-videos", "title": "X - Videos",
			"entries": [{"id": "ok1", "url": "https://www.youtube.com/watch?v=ok1", "title": "OK", "ie_key": "Youtube"}]
		}`,
		// @x/streams intentionally absent -> the mock errors for it.
	}, nil)

	listing, err := c.ListChannelVideos(context.Background(), "https://www.youtube.com/@x", 10)
	if err != nil {
		t.Fatalf("expected nil error despite failing tab, got %v", err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].ID != "ok1" {
		t.Fatalf("expected the healthy tab's entry, got %+v", listing.Entries)
	}
}

func TestListChannelVideos_NoLimitOmitsPlaylistEnd(t *testing.T) {
	var calls [][]string
	c := New()
	c.execFn = channelMock(t, map[string]string{
		"https://example.com/playlist/1": `{"id": "p1", "title": "P", "entries": []}`,
	}, &calls)

	if _, err := c.ListChannelVideos(context.Background(), "https://example.com/playlist/1", 0); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if strings.Contains(strings.Join(calls[0], " "), "--playlist-end") {
		t.Fatalf("expected no --playlist-end for limit=0: %v", calls[0])
	}
}
