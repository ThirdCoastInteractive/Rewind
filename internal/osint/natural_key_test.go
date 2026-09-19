package osint

import (
	"testing"

	"github.com/google/uuid"
)

func TestFlagNaturalKeyStable(t *testing.T) {
	c := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	v := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	k1 := FlagNaturalKey("newcomer", &c, &v, nil, "")
	k2 := FlagNaturalKey("newcomer", &c, &v, nil, "")
	if k1 != k2 {
		t.Fatalf("unstable key %q vs %q", k1, k2)
	}
	k3 := FlagNaturalKey("toxicity_burst", &c, nil, nil, "")
	if k1 == k3 {
		t.Fatal("different kinds must differ")
	}
}

func TestDismissedFlagsNotReinsertedUsesNaturalKey(t *testing.T) {
	// Unit-test the natural-key function the upsert path consults: same key
	// means a dismissed row blocks re-insert (store.upsertFlag checks exists).
	a := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	b := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	k := SockNaturalKey(a, b)
	if k != SockNaturalKey(b, a) {
		t.Fatal("sock key must be order-independent")
	}
	if FlagNaturalKey("sock_suggest", &a, nil, nil, b.String()) != k {
		// SockNaturalKey orders pair first; raw FlagNaturalKey with unordered ids differs — ensure helper is used.
		lo, hi := OrderedCommenterPair(a, b)
		if FlagNaturalKey("sock_suggest", &lo, nil, nil, hi.String()) != k {
			t.Fatal("sock natural key composition")
		}
	}
}

func TestSockNaturalKeysDistinctForSharedLo(t *testing.T) {
	lo := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	b := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	c := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	ab := SockNaturalKey(lo, b)
	ac := SockNaturalKey(lo, c)
	if ab == ac {
		t.Fatal("pairs that share the lower id must not share a natural key")
	}
}
