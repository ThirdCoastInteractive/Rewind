package osint

import "testing"

func TestSimhashIdenticalForCopyPasteVariants(t *testing.T) {
	a := NormalizeText("This is a coordinated message please share widely now.")
	b := NormalizeText("This   is a coordinated message  please share widely now.")
	ha, hb := Simhash64(a), Simhash64(b)
	if ha != hb {
		t.Fatalf("expected identical simhash, got %d vs %d (hamming %d)", ha, hb, HammingDistance(ha, hb))
	}
}

func TestHammingDistance(t *testing.T) {
	if HammingDistance(0, 0) != 0 {
		t.Fatal("zero")
	}
	if HammingDistance(0b1010, 0b1000) != 1 {
		t.Fatal("one bit")
	}
}
