package shownote

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyUnifiedDiff(t *testing.T) {
	original := "# Rundown\n\n- [One](rewind://video/1)\n"
	patch := "--- show.md\n+++ show.md\n@@ -1,3 +1,4 @@\n # Rundown\n \n - [One](rewind://video/1)\n+- [Two](rewind://video/2)\n"
	updated, err := ApplyUnifiedDiff(original, patch)
	require.NoError(t, err)
	require.Equal(t, "# Rundown\n\n- [One](rewind://video/1)\n- [Two](rewind://video/2)\n", updated)
}

func TestApplyUnifiedDiffRejectsMismatch(t *testing.T) {
	_, err := ApplyUnifiedDiff("one\n", "@@ -1 +1 @@\n-two\n+three\n")
	require.ErrorContains(t, err, "deletion does not match")
}

func TestMarkdownRangeUsesUTF16Positions(t *testing.T) {
	selected, start, end, err := MarkdownRange("a😀b\nnext", 1, 2, 1, 3)
	require.NoError(t, err)
	require.Equal(t, "😀", selected)
	require.Equal(t, 1, start)
	require.Equal(t, 3, end)
}

func TestChangedTextRangeAndUTF16Slice(t *testing.T) {
	change := ChangedTextRange("Before 😀 text\nAfter", "Before 😀 new text\nAfter")
	require.Equal(t, "", change.Before)
	require.Equal(t, "new ", change.After)
	require.Equal(t, 1, change.StartLine)
	selected, err := UTF16Slice("Before 😀 text\nAfter", change.StartUTF16, change.EndUTF16)
	require.NoError(t, err)
	require.Equal(t, change.Before, selected)
	_, err = UTF16Slice("😀", 1, 2)
	require.Error(t, err)
}
