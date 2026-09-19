package archival

import (
	"testing"

	"github.com/stretchr/testify/require"
	"thirdcoast.systems/rewind/internal/db"
)

func TestLiveChannelJobReusable(t *testing.T) {
	cases := []struct {
		name string
		job  *db.DownloadJob
		want bool
	}{
		{"nil", nil, false},
		{"queued", &db.DownloadJob{Status: "queued"}, true},
		{"processing", &db.DownloadJob{Status: "processing"}, true},
		{"succeeded", &db.DownloadJob{Status: "succeeded"}, false},
		{"failed", &db.DownloadJob{Status: "failed"}, false},
		{"cancelled", &db.DownloadJob{Status: "cancelled"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, liveChannelJobReusable(tc.job))
		})
	}
}

func TestLiveExtraArgs(t *testing.T) {
	cases := []struct {
		name      string
		fromStart bool
		wait      int
		mediaURL  string
		want      []string
	}{
		{
			name: "empty",
			want: []string{},
		},
		{
			name:      "from start only",
			fromStart: true,
			want:      []string{"--live-from-start"},
		},
		{
			name: "wait only",
			wait: 120,
			want: []string{"--wait-for-video", "120"},
		},
		{
			name:     "media url only",
			mediaURL: "https://cdn.example/live.m3u8",
			want:     []string{"--rewind-media-url", "https://cdn.example/live.m3u8"},
		},
		{
			name:      "all set",
			fromStart: true,
			wait:      60,
			mediaURL:  "http://cdn.example/a.m3u8",
			want: []string{
				"--live-from-start",
				"--wait-for-video", "60",
				"--rewind-media-url", "http://cdn.example/a.m3u8",
			},
		},
		{
			name: "negative wait ignored",
			wait: -5,
			want: []string{},
		},
		{
			name: "zero wait ignored",
			wait: 0,
			want: []string{},
		},
		{
			name: "wait clamped to max",
			wait: MaxWaitForVideoSeconds + 1,
			want: []string{"--wait-for-video", "86400"},
		},
		{
			name:     "whitespace media trimmed",
			mediaURL: "  https://cdn.example/x  ",
			want:     []string{"--rewind-media-url", "https://cdn.example/x"},
		},
		{
			name:     "invalid scheme omitted",
			mediaURL: "ftp://cdn.example/x",
			want:     []string{},
		},
		{
			name:     "empty host omitted",
			mediaURL: "https:///path",
			want:     []string{},
		},
		{
			name:     "junk media omitted",
			mediaURL: "not-a-url",
			want:     []string{},
		},
		{
			name:      "from start with junk media",
			fromStart: true,
			mediaURL:  "javascript:alert(1)",
			want:      []string{"--live-from-start"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := LiveExtraArgs(tc.fromStart, tc.wait, tc.mediaURL)
			require.Equal(t, tc.want, got)
			require.NotNil(t, got)
		})
	}
}
