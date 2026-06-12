package commentfmt

import "testing"

func TestParseSegments_NoTimestamp(t *testing.T) {
	got := ParseSegments("just a normal comment")
	if len(got) != 1 || got[0].IsTime || got[0].Text != "just a normal comment" {
		t.Fatalf("expected single plain segment, got %#v", got)
	}
}

func TestParseSegments_Empty(t *testing.T) {
	if got := ParseSegments(""); got != nil {
		t.Fatalf("expected nil for empty, got %#v", got)
	}
}

func TestParseSegments_Timestamps(t *testing.T) {
	segs := ParseSegments("intro at 1:23 then the good part 1:02:03 lol")
	// Expect: "intro at ", [1:23], " then the good part ", [1:02:03], " lol"
	var times []Seg
	for _, s := range segs {
		if s.IsTime {
			times = append(times, s)
		}
	}
	if len(times) != 2 {
		t.Fatalf("expected 2 timestamps, got %d (%#v)", len(times), segs)
	}
	if times[0].Text != "1:23" || times[0].Seconds != 83 {
		t.Errorf("1:23 => %v sec (%q)", times[0].Seconds, times[0].Text)
	}
	if times[1].Text != "1:02:03" || times[1].Seconds != 3723 {
		t.Errorf("1:02:03 => %v sec (%q)", times[1].Seconds, times[1].Text)
	}
}

func TestParseSegments_RoundTripText(t *testing.T) {
	in := "see 0:05 and 12:34 here"
	var b string
	for _, s := range ParseSegments(in) {
		b += s.Text
	}
	if b != in {
		t.Fatalf("segments don't reconstruct input: %q != %q", b, in)
	}
}

func TestParseSegments_NotRatios(t *testing.T) {
	// "16:9" and "2:1" are not valid M:SS timestamps (single-digit seconds).
	for _, s := range ParseSegments("aspect 16:9 ratio 2:1") {
		if s.IsTime {
			t.Errorf("unexpected timestamp match: %q", s.Text)
		}
	}
}

func TestParseSegments_InvalidSeconds(t *testing.T) {
	// 99 seconds is invalid; should stay plain text.
	for _, s := range ParseSegments("bogus 1:99 time") {
		if s.IsTime {
			t.Errorf("unexpected timestamp match: %q", s.Text)
		}
	}
}

func TestSafeHighlight_EscapesAndMarks(t *testing.T) {
	// text contained <b>, with the middle word highlighted by sentinels.
	in := "a <b> \x02match\x03 here"
	got := SafeHighlight(in)
	want := "a &lt;b&gt; <mark>match</mark> here"
	if got != want {
		t.Fatalf("SafeHighlight = %q, want %q", got, want)
	}
}

func TestSafeHighlight_NoInjection(t *testing.T) {
	got := SafeHighlight("<script>alert(1)</script>")
	if got != "&lt;script&gt;alert(1)&lt;/script&gt;" {
		t.Fatalf("script not neutralised: %q", got)
	}
}
