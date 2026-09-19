package wiki

import "testing"

const sampleVideo = "11111111-1111-1111-1111-111111111111"
const sampleClip = "22222222-2222-2222-2222-222222222222"

func TestParseRewindURI(t *testing.T) {
	m, ok := ParseRewindURI("rewind://video/" + sampleVideo)
	if !ok || m.Kind != "video" || m.ID != sampleVideo || m.Asset != "" {
		t.Fatalf("video: %#v ok=%v", m, ok)
	}
	m, ok = ParseRewindURI("rewind://video/" + sampleVideo + "#t=12.5,40")
	if !ok || !m.HasStart || !m.HasEnd || m.Start != 12.5 || m.End != 40 {
		t.Fatalf("range: %#v", m)
	}
	m, ok = ParseRewindURI("rewind://video/" + sampleVideo + "/thumbnail")
	if !ok || m.Asset != "thumbnail" || m.Image == "" {
		t.Fatalf("thumb: %#v", m)
	}
	m, ok = ParseRewindURI("rewind://video/" + sampleVideo + "/frame?t=3.25&quality=detail")
	if !ok || m.Asset != "frame" || m.Start != 3.25 || m.Image == "" {
		t.Fatalf("frame: %#v", m)
	}
	m, ok = ParseRewindURI("rewind://clip/" + sampleClip)
	if !ok || m.Kind != "clip" || m.ID != sampleClip {
		t.Fatalf("clip: %#v", m)
	}
	if _, ok := ParseRewindURI("https://example.com/x"); ok {
		t.Fatal("http should not parse")
	}
}

func TestRenderVideoEmbed(t *testing.T) {
	html := RenderHTML("![cold open](rewind://video/"+sampleVideo+")", TreeTopic)
	for _, need := range []string{
		`class="wiki-media wiki-media-video"`,
		`/api/videos/` + sampleVideo + `/stream`,
		`/api/videos/` + sampleVideo + `/thumbnail`,
		`cold open`,
		`<video`,
	} {
		if !contains(html, need) {
			t.Fatalf("missing %q in %s", need, html)
		}
	}
}

func TestRenderVideoRange(t *testing.T) {
	html := RenderHTML("![bit](rewind://video/"+sampleVideo+"#t=12,40)", TreeTopic)
	for _, need := range []string{`data-start="12.000"`, `data-end="40.000"`, `0:12–0:40`} {
		if !contains(html, need) {
			t.Fatalf("missing %q in %s", need, html)
		}
	}
}

func TestRenderThumbnail(t *testing.T) {
	html := RenderHTML("![still](rewind://video/"+sampleVideo+"/thumbnail)", TreeTopic)
	if contains(html, "<video") {
		t.Fatalf("thumbnail should not be a player: %s", html)
	}
	if !contains(html, `/api/videos/`+sampleVideo+`/thumbnail`) || !contains(html, "wiki-media-image") {
		t.Fatalf("still: %s", html)
	}
}

func TestRenderClipEmbed(t *testing.T) {
	lookup := func(id string) (ClipRef, bool) {
		if id != sampleClip {
			return ClipRef{}, false
		}
		return ClipRef{VideoID: sampleVideo, Title: "Guest walk-on", Start: 18, End: 42}, true
	}
	html := RenderHTMLMedia("![clip](rewind://clip/"+sampleClip+")", TreeTopic, lookup)
	for _, need := range []string{
		`wiki-media-clip`,
		`/api/videos/` + sampleVideo + `/stream`,
		`data-start="18.000"`,
		`data-end="42.000"`,
		`Guest walk-on`,
	} {
		if !contains(html, need) {
			t.Fatalf("missing %q in %s", need, html)
		}
	}
}

func TestBlockLinkBecomesPlayer(t *testing.T) {
	html := RenderHTML("[The Show](rewind://video/"+sampleVideo+")\n", TreeTopic)
	if !contains(html, "<video") || !contains(html, "The Show") {
		t.Fatalf("block link: %s", html)
	}
}

func TestInlineLinkRewrittenToWebPath(t *testing.T) {
	html := RenderHTML("See [The Show](rewind://video/"+sampleVideo+") tonight.", TreeTopic)
	if contains(html, "<video") {
		t.Fatalf("inline should not embed: %s", html)
	}
	if !contains(html, `/videos/`+sampleVideo) {
		t.Fatalf("expected web path: %s", html)
	}
	if contains(html, "rewind://") {
		t.Fatalf("leftover rewind uri: %s", html)
	}
}

func TestMissingClipCard(t *testing.T) {
	html := RenderHTMLMedia("![gone](rewind://clip/"+sampleClip+")", TreeTopic, func(string) (ClipRef, bool) {
		return ClipRef{}, false
	})
	if !contains(html, "clip not found") {
		t.Fatalf("missing card: %s", html)
	}
}

func TestFormatSec(t *testing.T) {
	if got := formatSec(12); got != "0:12" {
		t.Fatal(got)
	}
	if got := formatSec(75); got != "1:15" {
		t.Fatal(got)
	}
	if got := formatSec(3723); got != "1:02:03" {
		t.Fatal(got)
	}
}
