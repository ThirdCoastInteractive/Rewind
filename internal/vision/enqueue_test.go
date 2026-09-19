package vision

import (
	"encoding/json"
	"testing"
)

func TestIndexRangeAcceptsQuotedSeconds(t *testing.T) {
	var in IndexRange
	if err := json.Unmarshal([]byte(`{"video_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","start":"0","end":"30","dense":true}`), &in); err != nil {
		t.Fatal(err)
	}
	if float64(in.Start) != 0 || float64(in.End) != 30 || !in.Dense {
		t.Fatalf("%+v", in)
	}
}
