package osint

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

func TestOrderedCommenterPairAlwaysAIDLess(t *testing.T) {
	a := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	b := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	lo, hi := OrderedCommenterPair(a, b)
	if bytes.Compare(lo[:], hi[:]) >= 0 {
		t.Fatalf("expected a_id < b_id, got %s >= %s", lo, hi)
	}
	lo2, hi2 := OrderedCommenterPair(b, a)
	if lo != lo2 || hi != hi2 {
		t.Fatal("order should be stable regardless of input order")
	}
}
