package vision

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

func TestNormalizeRejectsBadVectors(t *testing.T) {
	t.Parallel()
	cases := []string{
		`[]`,
		`[1,2,3]`,
		`[` + strings.Repeat("0,", 511) + `0]`,
		`[` + strings.Repeat("NaN,", 511) + `0]`,
		`"not-a-vector"`,
	}
	for _, raw := range cases {
		if _, err := Normalize(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	vector, err := Normalize("[1," + strings.Repeat("0,", 510) + "0]")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(vector, "[1") {
		t.Fatalf("normalized=%s", vector)
	}
}

func TestSearchConsolidatesNearbyHits(t *testing.T) {
	t.Parallel()
	video := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	other := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	rows := []*db.SearchVisualEmbeddingsRow{
		{VideoID: video, SampleTs: 10, Similarity: 0.9},
		{VideoID: video, SampleTs: 12, Similarity: 0.8},
		{VideoID: video, SampleTs: 25, Similarity: 0.7},
		{VideoID: other, SampleTs: 11, Similarity: 0.6},
	}
	got := consolidate(rows, 20)
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].SampleTs != 10 || got[1].SampleTs != 25 || got[2].VideoID != other {
		t.Fatalf("%+v", got)
	}
	got = consolidate(rows, 1)
	if len(got) != 1 || got[0].SampleTs != 10 {
		t.Fatalf("limit: %+v", got)
	}
}

func TestSearchInputQuotedLimit(t *testing.T) {
	t.Parallel()
	var in SearchInput
	if err := json.Unmarshal([]byte(`{"text":"white tesla","limit":"8"}`), &in); err != nil {
		t.Fatal(err)
	}
	if in.Text != "white tesla" || int(in.Limit) != 8 {
		t.Fatalf("%+v", in)
	}
}
