package ytdlp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatSelector(t *testing.T) {
	cases := []struct {
		name      string
		maxHeight int
		live      bool
		want      string
	}{
		{
			name:      "uncapped",
			maxHeight: 0,
			want:      "bestvideo[format_id!*=timeline]+bestaudio/best",
		},
		{
			name:      "negative treated as uncapped",
			maxHeight: -1,
			want:      "bestvideo[format_id!*=timeline]+bestaudio/best",
		},
		{
			name:      "cap 720",
			maxHeight: 720,
			want:      "bestvideo[height<=720][format_id!*=timeline]+bestaudio/best[height<=720]/worst",
		},
		{
			name:      "cap 1080",
			maxHeight: 1080,
			want:      "bestvideo[height<=1080][format_id!*=timeline]+bestaudio/best[height<=1080]/worst",
		},
		{
			name:      "cap 2160",
			maxHeight: 2160,
			want:      "bestvideo[height<=2160][format_id!*=timeline]+bestaudio/best[height<=2160]/worst",
		},
		{
			name:      "live uncapped",
			maxHeight: 0,
			live:      true,
			want:      "best",
		},
		{
			name:      "live cap 720",
			maxHeight: 720,
			live:      true,
			want:      "best[height<=720]/worst",
		},
		{
			name:      "live cap 1080",
			maxHeight: 1080,
			live:      true,
			want:      "best[height<=1080]/worst",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatSelector(tc.maxHeight, tc.live)
			require.Equal(t, tc.want, got)

			if tc.live {
				require.NotContains(t, got, "bestvideo")
				require.NotContains(t, got, "timeline")
				return
			}

			// The timeline-preview guard must be present in every VOD case so Rumble's
			// low-res seekbar clip can never hijack the bestvideo branch.
			require.Contains(t, got, "[format_id!*=timeline]")

			if tc.maxHeight > 0 {
				require.Contains(t, got, "[height<=")
			} else {
				require.NotContains(t, got, "height<=")
			}
		})
	}
}

func TestLiveDownloadArgs(t *testing.T) {
	cases := []struct {
		name string
		opts LiveOpts
		want []string
	}{
		{
			name: "from-now",
			opts: LiveOpts{},
			want: []string{
				"--no-live-from-start",
				"--downloader", "native",
				"--hls-use-mpegts",
				"--skip-unavailable-fragments",
			},
		},
		{
			name: "from-start",
			opts: LiveOpts{FromStart: true},
			want: []string{
				"--live-from-start",
				"--downloader", "native",
				"--hls-use-mpegts",
				"--skip-unavailable-fragments",
			},
		},
		{
			name: "wait",
			opts: LiveOpts{Wait: 300},
			want: []string{
				"--no-live-from-start",
				"--downloader", "native",
				"--hls-use-mpegts",
				"--skip-unavailable-fragments",
				"--wait-for-video", "300",
			},
		},
		{
			name: "from-start with wait",
			opts: LiveOpts{FromStart: true, Wait: 60},
			want: []string{
				"--live-from-start",
				"--downloader", "native",
				"--hls-use-mpegts",
				"--skip-unavailable-fragments",
				"--wait-for-video", "60",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := LiveDownloadArgs(tc.opts)
			require.Equal(t, tc.want, got)
			require.NotContains(t, got, "-f")
			require.NotContains(t, got, "--format")
		})
	}
}

func TestStripRewindExtraArgs(t *testing.T) {
	cases := []struct {
		name     string
		extra    []string
		wantURL  string
		wantRest []string
	}{
		{
			name:     "empty",
			extra:    nil,
			wantURL:  "",
			wantRest: []string{},
		},
		{
			name:     "no media url",
			extra:    []string{"--no-playlist", "--live-from-start"},
			wantURL:  "",
			wantRest: []string{"--no-playlist", "--live-from-start"},
		},
		{
			name:     "media url mid",
			extra:    []string{"--foo", "--rewind-media-url", "https://cdn.example/m3u8", "--bar"},
			wantURL:  "https://cdn.example/m3u8",
			wantRest: []string{"--foo", "--bar"},
		},
		{
			name:     "media url trailing without value",
			extra:    []string{"--rewind-media-url"},
			wantURL:  "",
			wantRest: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotRest := StripRewindExtraArgs(tc.extra)
			require.Equal(t, tc.wantURL, gotURL)
			require.Equal(t, tc.wantRest, gotRest)
		})
	}
}

func TestParseLiveExtraArgs(t *testing.T) {
	cases := []struct {
		name     string
		extra    []string
		wantOpts LiveOpts
		wantHint bool
		wantRest []string
	}{
		{
			name:     "none",
			extra:    []string{"--no-playlist"},
			wantOpts: LiveOpts{},
			wantHint: false,
			wantRest: []string{"--no-playlist"},
		},
		{
			name:     "from-start",
			extra:    []string{"--live-from-start", "--foo"},
			wantOpts: LiveOpts{FromStart: true},
			wantHint: true,
			wantRest: []string{"--foo"},
		},
		{
			name:     "no-from-start",
			extra:    []string{"--no-live-from-start"},
			wantOpts: LiveOpts{FromStart: false},
			wantHint: true,
			wantRest: []string{},
		},
		{
			name:     "wait",
			extra:    []string{"--wait-for-video", "120", "--bar"},
			wantOpts: LiveOpts{Wait: 120},
			wantHint: true,
			wantRest: []string{"--bar"},
		},
		{
			name:     "combined",
			extra:    []string{"--live-from-start", "--wait-for-video", "30", "--keep"},
			wantOpts: LiveOpts{FromStart: true, Wait: 30},
			wantHint: true,
			wantRest: []string{"--keep"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, hint, rest := ParseLiveExtraArgs(tc.extra)
			require.Equal(t, tc.wantOpts, opts)
			require.Equal(t, tc.wantHint, hint)
			require.Equal(t, tc.wantRest, rest)
		})
	}
}

func TestCurrentlyLive(t *testing.T) {
	cases := []struct {
		name string
		info *Info
		want bool
	}{
		{name: "nil", info: nil, want: false},
		{name: "is_live flag", info: &Info{IsLive: true}, want: true},
		{name: "live_status is_live", info: &Info{LiveStatus: "is_live"}, want: true},
		{name: "live_status is_upcoming", info: &Info{LiveStatus: "is_upcoming"}, want: true},
		{name: "was_live only", info: &Info{WasLive: true}, want: false},
		{name: "post_live", info: &Info{LiveStatus: "post_live", WasLive: true}, want: false},
		{name: "not_live", info: &Info{LiveStatus: "not_live"}, want: false},
		{name: "vod empty", info: &Info{}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.info.CurrentlyLive())
		})
	}
}
