package creatorlink

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

type fakeSuggestDB struct {
	sug      *db.CreatorSuggestion
	members  []*db.Channel
	creators map[string]*db.Creator
	linked   *db.LinkChannelsToCreatorParams
	created  *db.CreateCreatorParams
}

func (f *fakeSuggestDB) GetCreatorSuggestion(_ context.Context, id pgtype.UUID) (*db.CreatorSuggestion, error) {
	if f.sug == nil || f.sug.ID != id {
		return nil, pgx.ErrNoRows
	}
	cp := *f.sug
	return &cp, nil
}

func (f *fakeSuggestDB) ListCreatorSuggestionMembers(context.Context, pgtype.UUID) ([]*db.Channel, error) {
	return f.members, nil
}

func (f *fakeSuggestDB) GetCreator(_ context.Context, id pgtype.UUID) (*db.Creator, error) {
	for _, c := range f.creators {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, pgx.ErrNoRows
}

func (f *fakeSuggestDB) GetCreatorByNameCI(_ context.Context, name string) (*db.Creator, error) {
	for _, c := range f.creators {
		if c.Name == name {
			return c, nil
		}
	}
	return nil, pgx.ErrNoRows
}

func (f *fakeSuggestDB) CreateCreator(_ context.Context, arg *db.CreateCreatorParams) (*db.Creator, error) {
	f.created = arg
	id := mustID("cccccccc-cccc-cccc-cccc-cccccccccccc")
	cr := &db.Creator{ID: id, Name: arg.Name}
	if f.creators == nil {
		f.creators = map[string]*db.Creator{}
	}
	f.creators[id.String()] = cr
	return cr, nil
}

func (f *fakeSuggestDB) LinkChannelsToCreator(_ context.Context, arg *db.LinkChannelsToCreatorParams) error {
	f.linked = arg
	return nil
}

func (f *fakeSuggestDB) SetCreatorSuggestionStatus(_ context.Context, arg *db.SetCreatorSuggestionStatusParams) error {
	if f.sug == nil || f.sug.Status != "pending" {
		return nil
	}
	f.sug.Status = arg.Status
	return nil
}

func TestAcceptAddLinksMembersAndMarksAccepted(t *testing.T) {
	sugID := mustID("11111111-1111-1111-1111-111111111111")
	creatorID := mustID("22222222-2222-2222-2222-222222222222")
	chID := mustID("33333333-3333-3333-3333-333333333333")
	f := &fakeSuggestDB{
		sug: &db.CreatorSuggestion{
			ID:        sugID,
			Kind:      "add",
			Status:    "pending",
			CreatorID: creatorID,
		},
		members:  []*db.Channel{{ID: chID, Uploader: "Alt Channel"}},
		creators: map[string]*db.Creator{"x": {ID: creatorID, Name: "Main"}},
	}
	got, err := Accept(context.Background(), f, sugID)
	if err != nil {
		t.Fatal(err)
	}
	if got != creatorID {
		t.Fatalf("creator %v want %v", got, creatorID)
	}
	if f.sug.Status != "accepted" {
		t.Fatalf("status %q", f.sug.Status)
	}
	if f.linked == nil || len(f.linked.Ids) != 1 || f.linked.Ids[0] != chID {
		t.Fatalf("linked %#v", f.linked)
	}
}

func TestDismissMarksDismissedWithoutLinking(t *testing.T) {
	sugID := mustID("11111111-1111-1111-1111-111111111111")
	f := &fakeSuggestDB{
		sug: &db.CreatorSuggestion{ID: sugID, Kind: "new", Status: "pending", ProposedName: "New Person"},
	}
	if err := Dismiss(context.Background(), f, sugID); err != nil {
		t.Fatal(err)
	}
	if f.sug.Status != "dismissed" {
		t.Fatalf("status %q", f.sug.Status)
	}
	if f.linked != nil {
		t.Fatal("dismiss should not link channels")
	}
}

func TestAcceptRejectsNonPending(t *testing.T) {
	sugID := mustID("11111111-1111-1111-1111-111111111111")
	f := &fakeSuggestDB{
		sug: &db.CreatorSuggestion{ID: sugID, Kind: "add", Status: "accepted"},
	}
	_, err := Accept(context.Background(), f, sugID)
	if !errors.Is(err, ErrSuggestionNotPending) {
		t.Fatalf("err %v", err)
	}
}

func TestAcceptNewCreatesCreator(t *testing.T) {
	sugID := mustID("11111111-1111-1111-1111-111111111111")
	chID := mustID("33333333-3333-3333-3333-333333333333")
	f := &fakeSuggestDB{
		sug: &db.CreatorSuggestion{
			ID:           sugID,
			Kind:         "new",
			Status:       "pending",
			ProposedName: "Story Warz",
			Reason:       "outlink",
		},
		members: []*db.Channel{{ID: chID, Uploader: "Story Warz"}},
	}
	id, err := Accept(context.Background(), f, sugID)
	if err != nil {
		t.Fatal(err)
	}
	if !id.Valid {
		t.Fatal("missing creator id")
	}
	if f.created == nil || f.created.Name != "Story Warz" {
		t.Fatalf("created %#v", f.created)
	}
	if f.sug.Status != "accepted" {
		t.Fatalf("status %q", f.sug.Status)
	}
}

func mustID(s string) pgtype.UUID {
	u := uuid.MustParse(s)
	var id pgtype.UUID
	id.Bytes = u
	id.Valid = true
	return id
}
