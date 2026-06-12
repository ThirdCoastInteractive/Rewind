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
