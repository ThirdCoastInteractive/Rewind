package stitch_api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

func TestRequireStitchJobAccessAcceptsOwner(t *testing.T) {
	owner := uuid.New()
	var ownerID pgtype.UUID
	if err := ownerID.Scan(owner.String()); err != nil {
		t.Fatal(err)
	}
	job := &db.GetStitchJobRow{CreatedBy: ownerID}
	if err := requireStitchJobAccess(nil, nil, job, ownerID); err != nil {
		t.Fatalf("owner denied: %v", err)
	}
}
