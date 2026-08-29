package mediaformat

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		src  string
		dur  int
		live bool
		ls   string
		want string
	}{
		{"https://youtube.com/watch?v=1", 600, false, "", Video},
		{"https://youtube.com/watch?v=1", 600, true, "", Livestream},
		{"https://youtube.com/shorts/abc", 45, false, "", Short},
		{"https://youtube.com/watch?v=1", 30, false, "", Short},
		{"https://rumble.com/c/x/livestreams/foo", 4000, false, "", Livestream},
	}
	for _, c := range cases {
		if got := Classify(c.src, c.dur, c.live, c.ls); got != c.want {
			t.Errorf("Classify(%q,%d,%v,%q)=%q want %q", c.src, c.dur, c.live, c.ls, got, c.want)
		}
	}
}
