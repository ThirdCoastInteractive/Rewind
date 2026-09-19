package encode

import (
	"encoding/json"
	"testing"
)

func TestStitchSegmentJSONEmptyStringNumerics(t *testing.T) {
	t.Parallel()
	raw := `{
		"type":"title",
		"text":"CALLEN'S TESLA",
		"subtitle":"Ben Avery & Devan Costa",
		"duration":3.2,
		"start_ts":"",
		"end_ts":"",
		"font_size":"",
		"transition":""
	}`
	var s stitchSegmentJSON
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	if s.Type != "title" {
		t.Fatalf("type=%q", s.Type)
	}
	if float64(s.Duration) != 3.2 {
		t.Fatalf("duration=%v", s.Duration)
	}
	if float64(s.StartTs) != 0 || float64(s.EndTs) != 0 {
		t.Fatalf("start/end = %v/%v", s.StartTs, s.EndTs)
	}
}

func TestStitchSegmentJSONQuotedNumerics(t *testing.T) {
	t.Parallel()
	raw := `{"type":"clip","clip_id":"abc","start_ts":"11464.22","end_ts":"11477.37","duration":"13.15"}`
	var s stitchSegmentJSON
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	if float64(s.StartTs) != 11464.22 || float64(s.EndTs) != 11477.37 {
		t.Fatalf("got start=%v end=%v", s.StartTs, s.EndTs)
	}
}
