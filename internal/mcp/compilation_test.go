package mcp

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestSortAndMergePlanSegmentsSameVideoWithin15s(t *testing.T) {
	d := pgDate("2024-01-02")
	got := orderPlanSegments([]datedPlanSegment{
		{in: planSegmentInput{VideoID: "a", Start: 30, End: 40}, date: d},
		{in: planSegmentInput{VideoID: "a", Start: 10, End: 20}, date: d},
	}, true, true)
	if len(got) != 1 {
		t.Fatalf("got %d segments, want 1", len(got))
	}
	if got[0].in.Start != 10 || got[0].in.End != 40 {
		t.Fatalf("merged %v-%v", got[0].in.Start, got[0].in.End)
	}
}

func TestSortAndMergePlanSegmentsKeepsFarHits(t *testing.T) {
	d := pgDate("2024-01-02")
	got := orderPlanSegments([]datedPlanSegment{
		{in: planSegmentInput{VideoID: "a", Start: 10, End: 20}, date: d},
		{in: planSegmentInput{VideoID: "a", Start: 36, End: 50}, date: d},
	}, true, true)
	if len(got) != 2 {
		t.Fatalf("got %d segments, want 2", len(got))
	}
}

func TestSortAndMergePlanSegmentsByUploadDate(t *testing.T) {
	got := orderPlanSegments([]datedPlanSegment{
		{in: planSegmentInput{VideoID: "later", Start: 0, End: 5}, date: pgDate("2024-06-01")},
		{in: planSegmentInput{VideoID: "earlier", Start: 0, End: 5}, date: pgDate("2024-01-01")},
	}, true, true)
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].in.VideoID != "earlier" || got[1].in.VideoID != "later" {
		t.Fatalf("order %q then %q", got[0].in.VideoID, got[1].in.VideoID)
	}
}

func pgDate(s string) pgtype.Date {
	tm, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return pgtype.Date{Time: tm, Valid: true}
}
