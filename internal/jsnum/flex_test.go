package jsnum

import (
	"encoding/json"
	"testing"
)

func TestParseFloat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want float64
	}{
		{`12.5`, 12.5},
		{`"12.5"`, 12.5},
		{`""`, 0},
		{`null`, 0},
		{`"  "`, 0},
		{`0`, 0},
	}
	for _, tc := range cases {
		got, err := ParseFloat([]byte(tc.in))
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestFlexFieldsInStruct(t *testing.T) {
	t.Parallel()
	var s struct {
		End F `json:"end_ts"`
		N   I `json:"font_size"`
	}
	if err := json.Unmarshal([]byte(`{"end_ts":"","font_size":"56"}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.End != 0 || s.N != 56 {
		t.Fatalf("got %+v", s)
	}
}

func TestCoerceJSONArrayTitleCard(t *testing.T) {
	t.Parallel()
	in := []byte(`[{"type":"title","text":"CALLEN'S TESLA","duration":3.2,"start_ts":"","end_ts":"","font_size":"72","transition":""}]`)
	out := CoerceJSONArray(in)
	var segs []map[string]any
	if err := json.Unmarshal(out, &segs); err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 {
		t.Fatalf("len=%d", len(segs))
	}
	if _, ok := segs[0]["start_ts"]; ok {
		t.Fatalf("start_ts should be omitted: %v", segs[0])
	}
	if _, ok := segs[0]["end_ts"]; ok {
		t.Fatalf("end_ts should be omitted: %v", segs[0])
	}
	if _, ok := segs[0]["transition"]; ok {
		t.Fatalf("empty transition should be omitted: %v", segs[0])
	}
	if segs[0]["duration"] != 3.2 {
		t.Fatalf("duration=%v", segs[0]["duration"])
	}
	if segs[0]["font_size"] != float64(72) && segs[0]["font_size"] != 72 {
		t.Fatalf("font_size=%T %v", segs[0]["font_size"], segs[0]["font_size"])
	}
}

func TestCoerceArgsQuotedToolNumbers(t *testing.T) {
	t.Parallel()
	args := map[string]any{
		"start":            "10.5",
		"end":              "20",
		"context_seconds":  "60",
		"revision":         "2",
		"segments":         []any{map[string]any{"start": "1", "end": "2"}},
	}
	got := CoerceArgs(args)
	if got["start"] != 10.5 || got["end"] != 20.0 {
		t.Fatalf("start/end %v %v", got["start"], got["end"])
	}
	if got["revision"] != 2 {
		t.Fatalf("revision=%T %v", got["revision"], got["revision"])
	}
	seg := got["segments"].([]any)[0].(map[string]any)
	if seg["start"] != 1.0 || seg["end"] != 2.0 {
		t.Fatalf("nested %v", seg)
	}
}
