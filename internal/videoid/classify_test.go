package videoid

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsPlaylistOrChannelURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		// Single videos.
		{"plain watch", "https://www.youtube.com/watch?v=ggLajT7aMMk", false},
		{"watch with list", "https://www.youtube.com/watch?v=ggLajT7aMMk&list=PL123", false},
		{"watch with list reversed order", "https://www.youtube.com/watch?list=PL123&v=ggLajT7aMMk", false},
		{"youtu.be short", "https://youtu.be/ggLajT7aMMk", false},
		{"youtu.be short with list", "https://youtu.be/ggLajT7aMMk?list=PL123", false},
		{"shorts", "https://www.youtube.com/shorts/ggLajT7aMMk", false},
		{"m. watch", "https://m.youtube.com/watch?v=ggLajT7aMMk", false},

		// Collections.
		{"playlist", "https://www.youtube.com/playlist?list=PL123", true},
		{"list without v", "https://www.youtube.com/feed?list=PL123", true},
		{"handle", "https://www.youtube.com/@SomeHandle", true},
		{"handle trailing slash", "https://www.youtube.com/@SomeHandle/", true},
		{"handle videos tab", "https://www.youtube.com/@SomeHandle/videos", true},
		{"handle search tab", "https://www.youtube.com/@TheFighterAndTheKid/search?query=tesla", true},
		{"site search results", "https://www.youtube.com/results?search_query=bryan+callen+tesla", true},
		{"channel", "https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv/", true},
		{"channel videos tab", "https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv/videos", true},
		{"c name", "https://www.youtube.com/c/SomeName", true},
		{"c name videos tab", "https://www.youtube.com/c/SomeName/videos", true},
		{"user name", "https://www.youtube.com/user/SomeUser", true},
		{"bare channel handle (schemeless)", "youtube.com/@SomeHandle", true},

		// Non-YouTube.
		{"non-youtube video", "https://www.twitch.tv/videos/123456789", false},
		{"non-youtube playlist path", "https://example.com/foo/playlist/bar", true},

		// Edge cases.
		{"empty", "", false},
		{"garbage", "://not a url", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsPlaylistOrChannelURL(tc.url), "url=%q", tc.url)
		})
	}
}

func TestIsLiveChannelURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		// Kick channel live pages.
		{"kick user", "https://kick.com/someuser", true},
		{"kick www", "https://www.kick.com/someuser", true},
		{"kick trailing slash", "https://kick.com/someuser/", true},

		// Kick non-live.
		{"kick vod", "https://kick.com/video/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", false},
		{"kick clips", "https://kick.com/user/clips/abc", false},
		{"kick categories", "https://kick.com/categories", false},
		{"kick search", "https://kick.com/search", false},
		{"kick auth", "https://kick.com/auth", false},

		// Twitch channel live pages.
		{"twitch user", "https://www.twitch.tv/someuser", true},
		{"twitch bare host", "https://twitch.tv/someuser", true},
		{"twitch trailing slash", "https://www.twitch.tv/someuser/", true},

		// Twitch non-live.
		{"twitch vod", "https://www.twitch.tv/videos/123456789", false},
		{"twitch clip path", "https://www.twitch.tv/someuser/clip/ClipSlug", false},
		{"twitch clips segment", "https://www.twitch.tv/clips", false},

		// YouTube /live under channel/handle.
		{"yt handle live", "https://www.youtube.com/@Handle/live", true},
		{"yt channel live", "https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv/live", true},
		{"yt c live", "https://youtube.com/c/Name/live", true},

		// YouTube non-live.
		{"yt bare live hub", "https://www.youtube.com/live", false},
		{"yt watch", "https://www.youtube.com/watch?v=ggLajT7aMMk", false},
		{"yt youtu.be", "https://youtu.be/ggLajT7aMMk", false},
		{"yt shorts", "https://www.youtube.com/shorts/ggLajT7aMMk", false},
		{"yt handle only", "https://www.youtube.com/@Handle", false},
		{"yt handle videos", "https://www.youtube.com/@Handle/videos", false},
		{"yt channel only", "https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv", false},
		{"yt playlist", "https://www.youtube.com/playlist?list=PL123", false},

		// Edge cases.
		{"empty", "", false},
		{"garbage", "://not a url", false},
		{"other host", "https://example.com/someuser", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsLiveChannelURL(tc.url), "url=%q", tc.url)
		})
	}
}
