package main

import (
	"testing"

	"thirdcoast.systems/rewind/pkg/ytdlp"
)

func TestChildDownloadURL(t *testing.T) {
	cases := []struct {
		name   string
		domain string
		entry  ytdlp.FlatEntry
		want   string
	}{
		{"full url passes through", "youtube.com", ytdlp.FlatEntry{ID: "abc", URL: "https://www.youtube.com/watch?v=abc"}, "https://www.youtube.com/watch?v=abc"},
		{"youtube reconstructs from id when url empty", "youtube.com", ytdlp.FlatEntry{ID: "abc", URL: ""}, "https://www.youtube.com/watch?v=abc"},
		{"youtube reconstructs when url is a bare id", "youtube.com", ytdlp.FlatEntry{ID: "abc", URL: "abc"}, "https://www.youtube.com/watch?v=abc"},
		{"non-youtube full url passes through", "vimeo.com", ytdlp.FlatEntry{ID: "123", URL: "https://vimeo.com/123"}, "https://vimeo.com/123"},
	}
	for _, tc := range cases {
		if got := childDownloadURL(tc.domain, tc.entry); got != tc.want {
			t.Errorf("%s: childDownloadURL(%q, %+v) = %q, want %q", tc.name, tc.domain, tc.entry, got, tc.want)
		}
	}
}
