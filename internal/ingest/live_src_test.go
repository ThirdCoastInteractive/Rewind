package ingest

import (
	"strings"
	"testing"
)

func TestResolveVideosSrc(t *testing.T) {
	kickUUID := "01234567-89ab-cdef-0123-456789abcdef"
	kickUUID2 := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	cases := []struct {
		name          string
		in            videosSrcInput
		wantSrc       string
		wantSrcPrefix string
		mustOmit      []string
		mustInclude   []string
	}{
		{
			name: "kick live uuid: src is video URL, channel omitted from candidates",
			in: videosSrcInput{
				RawJobURL:       "https://kick.com/someuser",
				ExpandedJobURL:  "https://kick.com/someuser",
				JobDerivedSrc:   "https://kick.com/someuser",
				WebpageURL:      "https://kick.com/someuser",
				OriginalURL:     "https://kick.com/someuser",
				InfoID:          kickUUID,
				CanonicalDomain: "kick.com",
				LiveStatus:      "is_live",
			},
			wantSrc:     "https://kick.com/video/" + kickUUID,
			mustOmit:    []string{"https://kick.com/someuser"},
			mustInclude: []string{"https://kick.com/video/" + kickUUID},
		},
		{
			name: "kick was_live slug: per-session livestreams URL, not channel",
			in: videosSrcInput{
				RawJobURL:       "https://kick.com/someuser",
				ExpandedJobURL:  "https://kick.com/someuser",
				JobDerivedSrc:   "https://kick.com/someuser",
				WebpageURL:      "https://kick.com/someuser",
				InfoID:          "stream-slug-42",
				CanonicalDomain: "kick.com",
				LiveStatus:      "was_live",
			},
			wantSrc:  "https://kick.com/livestreams/stream-slug-42",
			mustOmit: []string{"https://kick.com/someuser"},
		},
		{
			name: "youtube live already specific watch URL stays",
			in: videosSrcInput{
				RawJobURL:       "https://www.youtube.com/watch?v=abc12345678",
				ExpandedJobURL:  "https://www.youtube.com/watch?v=abc12345678",
				JobDerivedSrc:   "https://youtube.com/watch?v=abc12345678",
				WebpageURL:      "https://www.youtube.com/watch?v=abc12345678",
				InfoID:          "abc12345678",
				CanonicalDomain: "youtube.com",
				LiveStatus:      "is_live",
			},
			wantSrc: "https://youtube.com/watch?v=abc12345678",
		},
		{
			name: "vod: job URL preferred, no live override",
			in: videosSrcInput{
				RawJobURL:       "https://www.youtube.com/watch?v=abc12345678",
				ExpandedJobURL:  "https://www.youtube.com/watch?v=abc12345678",
				JobDerivedSrc:   "https://youtube.com/watch?v=abc12345678",
				WebpageURL:      "https://www.youtube.com/watch?v=otherid12345",
				InfoID:          "abc12345678",
				CanonicalDomain: "youtube.com",
			},
			wantSrc: "https://youtube.com/watch?v=abc12345678",
		},
		{
			name: "second kick session does not candidate the shared channel URL",
			in: videosSrcInput{
				RawJobURL:       "https://kick.com/someuser",
				ExpandedJobURL:  "https://kick.com/someuser",
				JobDerivedSrc:   "https://kick.com/someuser",
				WebpageURL:      "https://kick.com/someuser",
				InfoID:          kickUUID2,
				CanonicalDomain: "kick.com",
				IsLive:          true,
			},
			wantSrc:  "https://kick.com/video/" + kickUUID2,
			mustOmit: []string{"https://kick.com/someuser", "https://kick.com/video/" + kickUUID},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, cands := resolveVideosSrc(tc.in)
			if tc.wantSrc != "" && src != tc.wantSrc {
				t.Fatalf("src=%q want %q", src, tc.wantSrc)
			}
			if tc.wantSrcPrefix != "" && !strings.HasPrefix(src, tc.wantSrcPrefix) {
				t.Fatalf("src=%q want prefix %q", src, tc.wantSrcPrefix)
			}
			for _, omit := range tc.mustOmit {
				for _, c := range cands {
					if c == omit {
						t.Fatalf("candidates %v must omit %q", cands, omit)
					}
				}
			}
			for _, need := range tc.mustInclude {
				found := false
				for _, c := range cands {
					if c == need {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("candidates %v must include %q", cands, need)
				}
			}
			// Live channel job URL must never remain as src when an id exists.
			if infoIsLiveSession(tc.in.LiveStatus, tc.in.IsLive, tc.in.WasLive) &&
				strings.TrimSpace(tc.in.InfoID) != "" &&
				isLiveChannelPageURL(src) {
				t.Fatalf("live src must not be channel page: %q", src)
			}
		})
	}
}

func TestIsLiveChannelPageURL(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"https://kick.com/someuser", true},
		{"https://kick.com/someuser/videos", true},
		{"https://kick.com/video/" + "01234567-89ab-cdef-0123-456789abcdef", false},
		{"https://kick.com/livestreams/stream-slug-42", false},
		{"https://twitch.tv/someuser", true},
		{"https://twitch.tv/videos/123456789", false},
		{"https://youtube.com/@handle/live", true},
		{"https://youtube.com/live", true},
		{"https://youtube.com/live/abc12345678", false},
		{"https://youtube.com/watch?v=abc12345678", false},
	}
	for _, tc := range cases {
		if got := isLiveChannelPageURL(tc.raw); got != tc.want {
			t.Errorf("isLiveChannelPageURL(%q)=%v want %v", tc.raw, got, tc.want)
		}
	}
}

func TestTwoKickLivesDistinctSrc(t *testing.T) {
	channel := "https://kick.com/someuser"
	a := videosSrcInput{
		RawJobURL: channel, ExpandedJobURL: channel, JobDerivedSrc: channel,
		WebpageURL: channel, InfoID: "01234567-89ab-cdef-0123-456789abcdef",
		CanonicalDomain: "kick.com", LiveStatus: "is_live",
	}
	b := videosSrcInput{
		RawJobURL: channel, ExpandedJobURL: channel, JobDerivedSrc: channel,
		WebpageURL: channel, InfoID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		CanonicalDomain: "kick.com", LiveStatus: "is_live",
	}
	srcA, candsA := resolveVideosSrc(a)
	srcB, candsB := resolveVideosSrc(b)
	if srcA == srcB {
		t.Fatalf("sessions collided on src %q", srcA)
	}
	for _, cands := range [][]string{candsA, candsB} {
		for _, c := range cands {
			if c == channel {
				t.Fatalf("channel URL leaked into candidates: %v", cands)
			}
		}
	}
}
