package ytdlp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatSelector(t *testing.T) {
	cases := []struct {
		name      string
		maxHeight int
		want      string
	}{
		{
			name:      "uncapped",
			maxHeight: 0,
			want:      "bestvideo[format_id!*=timeline]+mergeall[vcodec=none]/best",
		},
		{
			name:      "negative treated as uncapped",
			maxHeight: -1,
			want:      "bestvideo[format_id!*=timeline]+mergeall[vcodec=none]/best",
		},
		{
			name:      "cap 720",
			maxHeight: 720,
			want:      "bestvideo[height<=720][format_id!*=timeline]+mergeall[vcodec=none]/best[height<=720]/worst",
		},
		{
			name:      "cap 1080",
			maxHeight: 1080,
			want:      "bestvideo[height<=1080][format_id!*=timeline]+mergeall[vcodec=none]/best[height<=1080]/worst",
		},
		{
			name:      "cap 2160",
			maxHeight: 2160,
			want:      "bestvideo[height<=2160][format_id!*=timeline]+mergeall[vcodec=none]/best[height<=2160]/worst",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatSelector(tc.maxHeight)
			require.Equal(t, tc.want, got)

			// The timeline-preview guard must be present in every case so Rumble's
			// low-res seekbar clip can never hijack the bestvideo branch.
			require.Contains(t, got, "[format_id!*=timeline]")

			if tc.maxHeight > 0 {
				require.Contains(t, got, "[height<=")
			} else {
				require.NotContains(t, got, "height<=")
			}
		})
	}
}
