package static

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewStaticCache_BuildsEntries(t *testing.T) {
	cache, err := NewStaticCache()
	require.NoError(t, err)
	require.NotNil(t, cache)
	require.NotEmpty(t, cache.entries)

	ci, ok := cache.entries["dist/main.css"]
	require.True(t, ok, "expected dist/main.css to be embedded")

	require.NotEmpty(t, ci.ETag)
	require.True(t, regexp.MustCompile(`^\"[0-9a-f]{64}\"$`).MatchString(ci.ETag))
	require.True(t, ci.Size > 0)
	require.False(t, ci.LastModified.IsZero())
}
