package shownote

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMarkdownCanonicalRundown(t *testing.T) {
	input := "# Cold open\n\n1. [Saved opener](rewind://clip/clip-1) @ 00:18–00:42\n   Strong cold open. Let it breathe.\n\n2. [The source](https://youtu.be/abc123) @ 1:02:34..1:03:10\n   Start after the pause.\n\n> Break — sponsor read\n"
	doc := ParseMarkdown(input)
	require.Len(t, doc.References, 2)
	require.Equal(t, ReferenceClip, doc.References[0].Kind)
	require.Equal(t, "clip-1", doc.References[0].ObjectID)
	require.Equal(t, float64(18), doc.References[0].Bounds.Start)
	require.Equal(t, float64(42), doc.References[0].Bounds.End)
	require.Equal(t, "Strong cold open. Let it breathe.", doc.References[0].Context)
	require.Equal(t, []string{"Cold open"}, doc.References[0].SectionPath)
	require.Equal(t, ReferenceExternal, doc.References[1].Kind)
	require.Equal(t, float64(3754), doc.References[1].Bounds.Start)
	require.Equal(t, float64(3790), doc.References[1].Bounds.End)
	require.Equal(t, ItemBreak, doc.Items[len(doc.Items)-1].Kind)
	require.Equal(t, "sponsor read", doc.Items[len(doc.Items)-1].Text)
}

func TestParseMarkdownBareURLAndMalformedTimestamp(t *testing.T) {
	doc := ParseMarkdown("https://example.com/video @ nope\n\n- [Bad](rewind://video/video-1) @ 0:99")
	require.Len(t, doc.References, 2)
	require.True(t, doc.References[0].Bare)
	require.Contains(t, doc.References[0].Diagnostic, "invalid timestamp")
	require.Contains(t, doc.References[1].Diagnostic, "invalid timestamp")
}

func TestParseMarkdownDuplicateOccurrencesRemainDistinct(t *testing.T) {
	doc := ParseMarkdown("[One](rewind://video/v1) @ 0:10\n\n[Two](rewind://video/v1) @ 0:20")
	require.Len(t, doc.References, 2)
	require.NotEqual(t, doc.References[0].OccurrenceKey, doc.References[1].OccurrenceKey)
}

func TestParseMarkdownRelativeReferencesAndUnicode(t *testing.T) {
	doc := ParseMarkdown("## 世界 🎙️\n- [Clip](/clips/c1) @ 1:00 - 1:30\n- [Marker](/markers/m1) @ 2:03\n- [Video](/videos/v1)")
	require.Len(t, doc.References, 3)
	require.Equal(t, ReferenceClip, doc.References[0].Kind)
	require.Equal(t, ReferenceMarker, doc.References[1].Kind)
	require.Equal(t, ReferenceVideo, doc.References[2].Kind)
	for _, ref := range doc.References {
		require.Equal(t, []string{"世界 🎙️"}, ref.SectionPath)
	}
}

func TestFormatTimestamp(t *testing.T) {
	require.Equal(t, "0:08", FormatTimestamp(8.9))
	require.Equal(t, "1:02:03", FormatTimestamp(3723))
}

func TestAddedExternalReferencesCountsOccurrences(t *testing.T) {
	before := "- [Existing](https://example.com/video) @ 0:10\n"
	after := before + "- [Duplicate](https://example.com/video) @ 0:20\n- [New](https://example.com/other)\n"

	added := AddedExternalReferences(before, after)
	require.Len(t, added, 2)
	require.Equal(t, "https://example.com/video", added[0].URI)
	require.Equal(t, "https://example.com/other", added[1].URI)
}

func TestRewriteReferenceBounds(t *testing.T) {
	markdown := "- [Saved](rewind://clip/clip-1) @ 0:10 - 0:20\n"
	updated, err := RewriteReferenceBounds(markdown, 1, "rewind://clip/clip-1", Bounds{
		Start: 18, End: 42, HasTime: true, IsRange: true,
	})
	require.NoError(t, err)
	require.Equal(t, "- [Saved](rewind://clip/clip-1) @ 0:18–0:42\n", updated)
}
