package osint

import "testing"

func TestStyleFeaturesStableOnSameText(t *testing.T) {
	texts := []string{
		"I think this is the best video you have made so far. Thank you!",
		"I think this is the best video you have made so far. Thank you!",
	}
	a := StyleFeatures(texts[:1])
	b := StyleFeatures(texts[:1])
	if StyleDistance(a, b) != 0 {
		t.Fatalf("same text should be identical: %#v vs %#v", a, b)
	}
	if a["function_word_rate"] <= 0 || a["type_token_ratio"] <= 0 {
		t.Fatalf("expected positive rates: %#v", a)
	}
	merged := MergeStyleFeatures(a, 1, b, 1)
	if StyleDistance(merged, a) > 1e-9 {
		t.Fatalf("merge of identical features drifted: %#v", merged)
	}
}
