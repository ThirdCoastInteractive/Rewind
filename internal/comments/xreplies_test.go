package comments

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

type fakeInfo struct {
	info *ytdlp.Info
	err  error
}

func (f fakeInfo) GetInfo(context.Context, string, ...string) (*ytdlp.Info, error) {
	return f.info, f.err
}

func TestIndexXRepliesFailsVisiblyWhenExtractorReturnsNone(t *testing.T) {
	t.Parallel()
	st := &fakeStore{}
	var videoID pgtype.UUID
	_ = videoID.Scan("22222222-2222-2222-2222-222222222222")
	err := IndexXReplies(context.Background(), st, fakeInfo{info: &ytdlp.Info{Raw: []byte(`{"id":"1","comments":[]}`)}}, videoID, "https://x.com/user/status/123")
	if !errors.Is(err, ErrNoReplies) {
		t.Fatalf("want ErrNoReplies, got %v", err)
	}
	if len(st.upserts) != 0 {
		t.Fatal("must not ingest empty replies as success")
	}
}

func TestIndexXRepliesRejectsMissingCommentsKey(t *testing.T) {
	t.Parallel()
	st := &fakeStore{}
	var videoID pgtype.UUID
	_ = videoID.Scan("22222222-2222-2222-2222-222222222222")
	err := IndexXReplies(context.Background(), st, fakeInfo{info: &ytdlp.Info{Raw: []byte(`{"id":"1"}`)}}, videoID, "https://twitter.com/user/status/999")
	if !errors.Is(err, ErrNoReplies) {
		t.Fatalf("want ErrNoReplies, got %v", err)
	}
}

func TestIndexXRepliesIngestsWhenPresent(t *testing.T) {
	t.Parallel()
	st := &fakeStore{}
	var videoID pgtype.UUID
	_ = videoID.Scan("22222222-2222-2222-2222-222222222222")
	raw := []byte(`{"id":"1","comments":[{"id":"r1","text":"reply","author":" ann ","author_id":"tw1"}]}`)
	if err := IndexXReplies(context.Background(), st, fakeInfo{info: &ytdlp.Info{Raw: raw}}, videoID, "https://x.com/user/status/123"); err != nil {
		t.Fatal(err)
	}
	if len(st.upserts) != 1 || st.upserts[0].Source != XSource {
		t.Fatalf("upserts=%#v", st.upserts)
	}
	if !st.attached["x.com|tw1"] {
		t.Fatalf("attached=%#v", st.attached)
	}
}

func TestIndexXRepliesRejectsNonStatusURL(t *testing.T) {
	t.Parallel()
	err := IndexXReplies(context.Background(), &fakeStore{}, fakeInfo{}, pgtype.UUID{}, "https://x.com/user")
	if err == nil {
		t.Fatal("expected error")
	}
}
