package osint

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// FlagNaturalKey builds a stable idempotency key for osint_flags upserts.
func FlagNaturalKey(kind string, commenterID, videoID, campaignID *uuid.UUID, extra string) string {
	parts := []string{kind}
	if commenterID != nil {
		parts = append(parts, "c:"+commenterID.String())
	}
	if videoID != nil {
		parts = append(parts, "v:"+videoID.String())
	}
	if campaignID != nil {
		parts = append(parts, "g:"+campaignID.String())
	}
	if extra != "" {
		parts = append(parts, "x:"+extra)
	}
	return strings.Join(parts, "|")
}

// SockNaturalKey keys a sock_suggest flag for an ordered commenter pair.
func SockNaturalKey(a, b uuid.UUID) string {
	lo, hi := OrderedCommenterPair(a, b)
	return FlagNaturalKey("sock_suggest", &lo, nil, nil, hi.String())
}

// RaidBucketKey identifies a raid window on a video.
func RaidBucketKey(videoID uuid.UUID, bucketStartUnix int64) string {
	return fmt.Sprintf("%s:%d", videoID.String(), bucketStartUnix)
}
