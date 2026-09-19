package channelid

import "testing"

func TestPlatformFromSrc(t *testing.T) {
	if PlatformFromSrc("https://www.youtube.com/watch?v=x") != "youtube" {
		t.Fatal("youtube")
	}
	if PlatformFromSrc("https://rumble.com/v123") != "rumble" {
		t.Fatal("rumble")
	}
	if PlatformFromSrc("https://x.com/SomeHandle/status/1") != "twitter" {
		t.Fatal("x.com")
	}
	if PlatformFromSrc("https://twitter.com/SomeHandle") != "twitter" {
		t.Fatal("twitter.com")
	}
}

func TestFromMetadataPrefersChannelID(t *testing.T) {
	ch := "UCabc"
	id := FromMetadata("https://youtube.com/watch?v=1", "Joe", &ch, nil, "https://youtube.com/channel/UCabc", "")
	if id.Platform != "youtube" || id.Key != "UCabc" {
		t.Fatalf("%+v", id)
	}
}

func TestFromURL(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		platform     string
		key          string
		channelID    string
		canonicalURL string
	}{
		{
			name:         "youtube channel id",
			raw:          "https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv",
			platform:     "youtube",
			key:          "UCabcdefghijklmnopqrstuv",
			channelID:    "UCabcdefghijklmnopqrstuv",
			canonicalURL: "https://youtube.com/channel/UCabcdefghijklmnopqrstuv",
		},
		{
			name:         "youtube channel id videos tab",
			raw:          "https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv/videos",
			platform:     "youtube",
			key:          "UCabcdefghijklmnopqrstuv",
			channelID:    "UCabcdefghijklmnopqrstuv",
			canonicalURL: "https://youtube.com/channel/UCabcdefghijklmnopqrstuv",
		},
		{
			name:         "youtube handle",
			raw:          "https://www.youtube.com/@SomeHandle",
			platform:     "youtube",
			key:          "@SomeHandle",
			canonicalURL: "https://youtube.com/@SomeHandle",
		},
		{
			name:         "youtube handle videos tab",
			raw:          "https://www.youtube.com/@SomeHandle/videos",
			platform:     "youtube",
			key:          "@SomeHandle",
			canonicalURL: "https://youtube.com/@SomeHandle",
		},
		{
			name:         "youtube handle schemeless",
			raw:          "youtube.com/@SomeHandle",
			platform:     "youtube",
			key:          "@SomeHandle",
			canonicalURL: "https://youtube.com/@SomeHandle",
		},
		{
			name:         "youtube custom c/",
			raw:          "https://www.youtube.com/c/SomeName",
			platform:     "youtube",
			key:          "c/SomeName",
			canonicalURL: "https://youtube.com/c/SomeName",
		},
		{
			name:         "youtube user/",
			raw:          "https://www.youtube.com/user/SomeUser",
			platform:     "youtube",
			key:          "user/SomeUser",
			canonicalURL: "https://youtube.com/user/SomeUser",
		},
		{
			name:         "youtube playlist",
			raw:          "https://www.youtube.com/playlist?list=PLabc123",
			platform:     "youtube",
			key:          "PLabc123",
			canonicalURL: "https://youtube.com/playlist?list=PLabc123",
		},
		{
			name:         "rumble c/",
			raw:          "https://rumble.com/c/ChannelName",
			platform:     "rumble",
			key:          "ChannelName",
			canonicalURL: "https://rumble.com/c/ChannelName",
		},
		{
			name:         "rumble numeric c/",
			raw:          "https://www.rumble.com/c/c-123456",
			platform:     "rumble",
			key:          "c-123456",
			canonicalURL: "https://rumble.com/c/c-123456",
		},
		{
			name:         "kick channel",
			raw:          "https://kick.com/coolstreamer",
			platform:     "kick",
			key:          "coolstreamer",
			canonicalURL: "https://kick.com/coolstreamer",
		},
		{
			name:         "kick channel videos tab",
			raw:          "https://www.kick.com/coolstreamer/videos",
			platform:     "kick",
			key:          "coolstreamer",
			canonicalURL: "https://kick.com/coolstreamer",
		},
		{
			name:         "x.com profile",
			raw:          "https://x.com/LuisJGomez",
			platform:     "twitter",
			key:          "LuisJGomez",
			canonicalURL: "https://x.com/LuisJGomez",
		},
		{
			name:         "twitter.com profile",
			raw:          "https://twitter.com/LuisJGomez",
			platform:     "twitter",
			key:          "LuisJGomez",
			canonicalURL: "https://x.com/LuisJGomez",
		},
		{
			name:         "generic host+path",
			raw:          "https://www.example.com/foo/bar/",
			platform:     "other",
			key:          "example.com/foo/bar",
			canonicalURL: "https://example.com/foo/bar",
		},
		{
			name:     "empty",
			raw:      "",
			platform: "other",
			key:      "unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := FromURL(tc.raw)
			if id.Platform != tc.platform || id.Key != tc.key || id.ChannelID != tc.channelID {
				t.Fatalf("identity: %+v", id)
			}
			if tc.canonicalURL != "" && id.CanonicalURL != tc.canonicalURL {
				t.Fatalf("canonical url: got %q want %q", id.CanonicalURL, tc.canonicalURL)
			}
		})
	}
}
