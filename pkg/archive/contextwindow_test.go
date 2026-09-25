package archive

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"thirdcoast.systems/rewind/internal/db"
)

type fakeContextWindows struct {
	set     *db.CreateContextWindowSetParams
	windows []*db.InsertGeneratedContextWindowParams
}

func (f *fakeContextWindows) CreateContextWindowSet(_ context.Context, arg *db.CreateContextWindowSetParams) (*db.ContextWindowSet, error) {
	f.set = arg
	id := pgUUID(uuid.MustParse("11111111-1111-1111-1111-111111111111"))
	return &db.ContextWindowSet{ID: id, VideoID: arg.VideoID, Status: arg.Status}, nil
}

func (f *fakeContextWindows) InsertGeneratedContextWindow(_ context.Context, arg *db.InsertGeneratedContextWindowParams) (*db.ContextWindow, error) {
	f.windows = append(f.windows, arg)
	return &db.ContextWindow{ID: pgUUID(uuid.MustParse("22222222-2222-2222-2222-222222222222")), SetID: arg.SetID, Ordinal: arg.Ordinal, Kind: arg.Kind}, nil
}

func (f *fakeContextWindows) MarkGeneratedWindowsStale(context.Context, *db.MarkGeneratedWindowsStaleParams) error {
	return nil
}

func TestImportContextWindowsSkipsNonPositiveSpans(t *testing.T) {
	w := &fakeContextWindows{}
	videoID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	err := importContextWindowsWith(context.Background(), w, videoID, []ContextWindow{
		{Start: 10, End: 5, Title: "backwards"},
		{Start: 3, End: 3, Title: "empty"},
		{Start: 1, End: 4, Title: "one", Summary: "first"},
		{Start: 4, End: 9, Title: "two", Summary: "second", Shorts: []ContextWindow{{Start: 5, End: 8, Title: "beat", Hook: "the line"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.set == nil {
		t.Fatal("set not created")
	}
	if w.set.VideoID.String() != videoID || w.set.Status != "succeeded" {
		t.Fatalf("set %+v", w.set)
	}
	if w.set.TranscriptHash != importedContextHash || w.set.ModelDigest != importedContextDigest || w.set.PromptVersion != importedContextPrompt {
		t.Fatalf("set key %+v", w.set)
	}
	if len(w.windows) != 3 {
		t.Fatalf("windows %d", len(w.windows))
	}
	if w.windows[2].Kind != "short" || w.windows[2].Hook != "the line" || w.windows[2].Ordinal != 10001 || !w.windows[2].ParentID.Valid {
		t.Fatalf("short %+v", w.windows[2])
	}
	setID := pgUUID(uuid.MustParse("11111111-1111-1111-1111-111111111111"))
	for i, got := range w.windows[:2] {
		if got.Ordinal != int32(i) {
			t.Fatalf("ordinal %d = %d", i, got.Ordinal)
		}
		if got.Kind != "window" || got.CueStart != 0 || got.CueEnd != 0 {
			t.Fatalf("window %d %+v", i, got)
		}
		if got.SetID != setID || got.VideoID.String() != videoID {
			t.Fatalf("ids %+v", got)
		}
		if got.Topics == nil || got.Entities == nil {
			t.Fatal("nil topic or entity slice")
		}
	}
	if w.windows[0].Title != "one" || w.windows[0].StartTs != 1 || w.windows[0].EndTs != 4 || w.windows[0].Summary != "first" {
		t.Fatalf("first %+v", w.windows[0])
	}
	if w.windows[1].Title != "two" || w.windows[1].EndTs != 9 {
		t.Fatalf("second %+v", w.windows[1])
	}

	skipped := &fakeContextWindows{}
	if err := importContextWindowsWith(context.Background(), skipped, videoID, []ContextWindow{{Start: 2, End: 2}}); err != nil {
		t.Fatal(err)
	}
	if skipped.set != nil || len(skipped.windows) != 0 {
		t.Fatal("non-positive span should not insert")
	}
	if err := importContextWindowsWith(context.Background(), skipped, "bad-id", []ContextWindow{{Start: 0, End: 1, Title: "x"}}); err == nil {
		t.Fatal("invalid video id")
	}
}
