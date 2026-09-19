package mcp

import (
	"encoding/json"
	"testing"

	"thirdcoast.systems/rewind/internal/vision"
)

func TestVisualSearchInputQuotedLimit(t *testing.T) {
	var in vision.SearchInput
	if err := json.Unmarshal([]byte(`{"text":"white tesla","limit":"8"}`), &in); err != nil {
		t.Fatal(err)
	}
	if in.Text != "white tesla" || int(in.Limit) != 8 {
		t.Fatalf("%+v", in)
	}
}

func TestVisualIndexRangeQuotedBounds(t *testing.T) {
	var in vision.IndexRange
	if err := json.Unmarshal([]byte(`{"video_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","start":"0","end":"30"}`), &in); err != nil {
		t.Fatal(err)
	}
	if in.Start != 0 || in.End != 30 {
		t.Fatalf("%+v", in)
	}
}
