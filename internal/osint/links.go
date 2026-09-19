package osint

import (
	"bytes"

	"github.com/google/uuid"
)

// OrderedCommenterPair returns (a,b) with a < b by UUID byte order.
// Style matches are hypotheses only; never auto-merge commenters.
func OrderedCommenterPair(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if bytes.Compare(a[:], b[:]) <= 0 {
		return a, b
	}
	return b, a
}
