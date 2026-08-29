package channellinks

import "testing"

func hitsByKind(hits []Hit, kind string) []Hit {
	var out []Hit
	for _, h := range hits {
		if h.Kind == kind {
			out = append(out, h)
		}
	}
	return out
}

func TestParseYouTubeHandleOutlink(t *testing.T) {
	hits := Parse("Collab with https://www.youtube.com/@OtherChannel/videos today")
	out := hitsByKind(hits, KindOutlink)
	if len(out) != 1 || out[0].URL != "https://youtube.com/@OtherChannel" {
		t.Fatalf("outlinks: %+v", hits)
	}
	if hitsByKind(hits, KindMention) != nil {
		t.Fatalf("handle inside URL should not also be a mention: %+v", hits)
	}
}

func TestParseYouTubeChannelUC(t *testing.T) {
	hits := Parse("see https://youtube.com/channel/UCabcdefghijklmnopqrstuv for more")
	out := hitsByKind(hits, KindOutlink)
	if len(out) != 1 || out[0].URL != "https://youtube.com/channel/UCabcdefghijklmnopqrstuv" {
		t.Fatalf("%+v", hits)
	}
}

func TestParseRumbleOutlink(t *testing.T) {
	hits := Parse("also on https://rumble.com/c/FooBar")
	out := hitsByKind(hits, KindOutlink)
	if len(out) != 1 || out[0].URL != "https://rumble.com/c/FooBar" {
		t.Fatalf("%+v", hits)
	}
}

func TestParseKickOutlink(t *testing.T) {
	hits := Parse("live at https://kick.com/someuser tonight")
	out := hitsByKind(hits, KindOutlink)
	if len(out) != 1 || out[0].URL != "https://kick.com/someuser" {
		t.Fatalf("%+v", hits)
	}
}

func TestParseKickIgnoresVideoPath(t *testing.T) {
	hits := Parse("clip https://kick.com/video/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if hitsByKind(hits, KindOutlink) != nil {
		t.Fatalf("kick video URL should not be an outlink: %+v", hits)
	}
}

func TestParseMentionDefaultsToTwitter(t *testing.T) {
	hits := Parse("thanks @CoolChannel for the clip")
	mentions := hitsByKind(hits, KindMention)
	if len(mentions) != 1 || mentions[0].URL != "https://x.com/CoolChannel" {
		t.Fatalf("%+v", hits)
	}
}

func TestParseMentionFromYouTubeSource(t *testing.T) {
	hits := ParseOn("youtube", "thanks @CoolChannel for the clip")
	mentions := hitsByKind(hits, KindMention)
	if len(mentions) != 1 || mentions[0].URL != "https://youtube.com/@CoolChannel" {
		t.Fatalf("%+v", hits)
	}
}

func TestParseTwitterProfileOutlink(t *testing.T) {
	hits := Parse("also https://twitter.com/reservoirfarms today")
	out := hitsByKind(hits, KindOutlink)
	if len(out) != 1 || out[0].URL != "https://x.com/reservoirfarms" {
		t.Fatalf("%+v", hits)
	}
}

func TestParseEmailIsNotMention(t *testing.T) {
	hits := Parse("write us at desk@example.com please")
	if hitsByKind(hits, KindMention) != nil {
		t.Fatalf("email local-part should not yield a mention: %+v", hits)
	}
}

func TestParseWatchURLIsNotOutlink(t *testing.T) {
	hits := Parse("watch https://www.youtube.com/watch?v=dQw4w9wgGcQ")
	if hitsByKind(hits, KindOutlink) != nil {
		t.Fatalf("watch URL should not be an outlink: %+v", hits)
	}
}
