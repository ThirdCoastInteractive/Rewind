package comments

import "testing"

func TestCommenterKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		authorID  string
		authorURL string
		want      string
	}{
		{name: "nonempty author_id wins", authorID: "UC123", authorURL: "https://www.youtube.com/channel/UCother", want: "UC123"},
		{name: "trim author_id", authorID: "  UC123  ", authorURL: "", want: "UC123"},
		{name: "uc from youtube url", authorID: "", authorURL: "https://www.youtube.com/channel/UCabcdef_-12", want: "UCabcdef_-12"},
		{name: "uc without www", authorID: "", authorURL: "https://youtube.com/channel/UCxyz", want: "UCxyz"},
		{name: "case insensitive host", authorID: "", authorURL: "https://WWW.YouTube.COM/channel/UCxyz", want: "UCxyz"},
		{name: "empty both", authorID: "", authorURL: "", want: ""},
		{name: "whitespace both", authorID: "   ", authorURL: "  ", want: ""},
		{name: "non-channel url ignored", authorID: "", authorURL: "https://youtube.com/@handle", want: ""},
		{name: "empty author_id prefers url", authorID: "   ", authorURL: "https://youtube.com/channel/UCabc", want: "UCabc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CommenterKey(tc.authorID, tc.authorURL); got != tc.want {
				t.Fatalf("CommenterKey(%q, %q) = %q, want %q", tc.authorID, tc.authorURL, got, tc.want)
			}
		})
	}
}
