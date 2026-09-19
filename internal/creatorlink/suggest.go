package creatorlink

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

// SuggestionDB is the store Accept and Dismiss need. *db.Queries implements it.
type SuggestionDB interface {
	GetCreatorSuggestion(ctx context.Context, id pgtype.UUID) (*db.CreatorSuggestion, error)
	ListCreatorSuggestionMembers(ctx context.Context, suggestionID pgtype.UUID) ([]*db.Channel, error)
	GetCreator(ctx context.Context, id pgtype.UUID) (*db.Creator, error)
	GetCreatorByNameCI(ctx context.Context, name string) (*db.Creator, error)
	CreateCreator(ctx context.Context, arg *db.CreateCreatorParams) (*db.Creator, error)
	LinkChannelsToCreator(ctx context.Context, arg *db.LinkChannelsToCreatorParams) error
	SetCreatorSuggestionStatus(ctx context.Context, arg *db.SetCreatorSuggestionStatusParams) error
}

// ErrSuggestionNotPending is returned when the nomination is missing or not open.
var ErrSuggestionNotPending = errors.New("creator suggestion is not pending")

// Accept applies a pending nomination the same way the Creators page does:
// link member channels, optionally create the creator, then mark accepted.
func Accept(ctx context.Context, q SuggestionDB, id pgtype.UUID) (pgtype.UUID, error) {
	sug, err := q.GetCreatorSuggestion(ctx, id)
	if err != nil || sug == nil || sug.Status != "pending" {
		return pgtype.UUID{}, ErrSuggestionNotPending
	}
	members, err := q.ListCreatorSuggestionMembers(ctx, sug.ID)
	if err != nil {
		return pgtype.UUID{}, err
	}
	ids := make([]pgtype.UUID, 0, len(members))
	for _, ch := range members {
		if ch != nil && ch.ID.Valid {
			ids = append(ids, ch.ID)
		}
	}
	var creatorID pgtype.UUID
	switch sug.Kind {
	case "add":
		if !sug.CreatorID.Valid {
			return pgtype.UUID{}, fmt.Errorf("add suggestion missing creator")
		}
		if _, err := q.GetCreator(ctx, sug.CreatorID); err != nil {
			return pgtype.UUID{}, err
		}
		creatorID = sug.CreatorID
	case "new":
		created, err := createCreatorFromSuggestion(ctx, q, sug)
		if err != nil {
			return pgtype.UUID{}, err
		}
		creatorID = created.ID
	default:
		return pgtype.UUID{}, fmt.Errorf("unknown creator suggestion kind %q", sug.Kind)
	}
	if len(ids) > 0 {
		if err := q.LinkChannelsToCreator(ctx, &db.LinkChannelsToCreatorParams{
			CreatorID: creatorID,
			Ids:       ids,
		}); err != nil {
			return pgtype.UUID{}, err
		}
	}
	if err := q.SetCreatorSuggestionStatus(ctx, &db.SetCreatorSuggestionStatusParams{
		Status: "accepted",
		ID:     sug.ID,
	}); err != nil {
		return pgtype.UUID{}, err
	}
	return creatorID, nil
}

// Dismiss marks a pending nomination dismissed without linking channels.
func Dismiss(ctx context.Context, q SuggestionDB, id pgtype.UUID) error {
	sug, err := q.GetCreatorSuggestion(ctx, id)
	if err != nil || sug == nil || sug.Status != "pending" {
		return ErrSuggestionNotPending
	}
	return q.SetCreatorSuggestionStatus(ctx, &db.SetCreatorSuggestionStatusParams{
		Status: "dismissed",
		ID:     sug.ID,
	})
}

func createCreatorFromSuggestion(ctx context.Context, q SuggestionDB, sug *db.CreatorSuggestion) (*db.Creator, error) {
	name := strings.TrimSpace(sug.ProposedName)
	if name == "" {
		name = "Untitled creator"
	}
	if existing, err := q.GetCreatorByNameCI(ctx, name); err == nil && existing != nil {
		return existing, nil
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	notes := strings.TrimSpace(sug.Reason)
	var notesArg *string
	if notes != "" {
		notesArg = &notes
	}
	return q.CreateCreator(ctx, &db.CreateCreatorParams{
		Name:  name,
		Notes: notesArg,
	})
}
