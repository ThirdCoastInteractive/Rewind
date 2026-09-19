package video_api

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

func TestFilterWatchContextWindows(t *testing.T) {
	rows := []*db.ListContextWindowsForVideoRow{
		{Title: "Buying a Tesla", Summary: "An old car", Topics: []string{"electric cars"}, Entities: []string{"Bryan Callen"}},
		{Title: "Other cars", Summary: "A new truck"},
	}
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"", 2}, {"  ", 2}, {"TESLA", 1}, {"bryan electric", 1},
		{"old Callen", 1}, {"truck tesla", 0}, {"' OR 1=1", 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := len(filterWatchContextWindows(rows, tc.query)); got != tc.want {
				t.Fatalf("got %d results, want %d", got, tc.want)
			}
		})
	}
	if len(filterWatchContextWindows(nil, "test")) != 0 {
		t.Fatal("empty video should have no matches")
	}
}

func TestFilterWatchContextWindowsNestsShorts(t *testing.T) {
	parentID := pgtype.UUID{}
	shortID := pgtype.UUID{}
	if err := parentID.Scan("11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatal(err)
	}
	if err := shortID.Scan("22222222-2222-2222-2222-222222222222"); err != nil {
		t.Fatal(err)
	}
	parent := &db.ListContextWindowsForVideoRow{ID: parentID, Title: "Studio Banter", Summary: "Setup talk", Kind: "window"}
	short := &db.ListContextWindowsForVideoRow{ID: shortID, ParentID: parentID, Title: "Casio punch", Hook: "that's the joke", Kind: "short"}
	other := &db.ListContextWindowsForVideoRow{Title: "Other cars", Summary: "A new truck", Kind: "window"}
	rows := []*db.ListContextWindowsForVideoRow{parent, short, other}

	got := filterWatchContextWindows(rows, "casio")
	if len(got) != 2 {
		t.Fatalf("short search should keep parent+short, got %d", len(got))
	}
	got = filterWatchContextWindows(rows, "studio")
	if len(got) != 2 {
		t.Fatalf("parent search should keep parent+its shorts, got %d", len(got))
	}
	got = filterWatchContextWindows(rows, "joke")
	if len(got) != 2 {
		t.Fatalf("hook search should keep parent+short, got %d", len(got))
	}
}
