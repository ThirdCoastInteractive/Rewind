package archive

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

var errNoVideo = errors.New("archive: no video for tenant stamp")

type fakeMasterIns struct {
	rows map[string]*db.Video
}

func (f *fakeMasterIns) InsertVideo(_ context.Context, p *db.InsertVideoParams) (*db.Video, error) {
	if f.rows == nil {
		f.rows = map[string]*db.Video{}
	}
	key := p.TenantID.String() + "|" + p.Src
	if existing, ok := f.rows[key]; ok {
		return existing, nil
	}
	v := &db.Video{ID: p.ID, Src: p.Src, TenantID: p.TenantID, ArchivedBy: p.ArchivedBy}
	f.rows[key] = v
	return v, nil
}

func (f *fakeMasterIns) SetVideoTenantID(_ context.Context, id, tenant pgtype.UUID) error {
	for _, v := range f.rows {
		if v.ID == id {
			v.TenantID = tenant
			return nil
		}
	}
	return errNoVideo
}

func TestImportMasterStampsTenantAndPersistedID(t *testing.T) {
	ins := &fakeMasterIns{}
	ctx := context.Background()
	src := "stream://same-show"
	tenantA := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	tenantB := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	id1, err := importMasterWith(ctx, ins, Master{TenantID: tenantA, Src: src, Title: "one"})
	if err != nil {
		t.Fatal(err)
	}
	id1b, err := importMasterWith(ctx, ins, Master{TenantID: tenantA, Src: src, Title: "again"})
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id1b {
		t.Fatalf("same tenant collision should keep first id: %s vs %s", id1, id1b)
	}
	id2, err := importMasterWith(ctx, ins, Master{TenantID: tenantB, Src: src, Title: "other tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if id2 == id1 {
		t.Fatal("second tenant must not overwrite first")
	}
	row2 := ins.rows[db.ParseTenant(tenantB).String()+"|"+src]
	if row2 == nil || row2.TenantID != db.ParseTenant(tenantB) {
		t.Fatalf("imported tenant %+v", row2)
	}
	if row2.ID.String() != id2 {
		t.Fatalf("returned id %s persisted %s", id2, row2.ID.String())
	}
	_ = pgtype.UUID{}
}

func TestImportMasterOSSTenant(t *testing.T) {
	ins := &fakeMasterIns{}
	id, err := importMasterWith(context.Background(), ins, Master{Src: "stream://oss", Title: "oss"})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty id")
	}
	var found *db.Video
	for _, v := range ins.rows {
		found = v
	}
	if found == nil || found.TenantID != db.OSSTenant() {
		t.Fatalf("oss tenant %+v", found)
	}
}

func TestImportMasterUsesProvidedIDOrGenerates(t *testing.T) {
	ins := &fakeMasterIns{}
	ctx := context.Background()
	want := "cccccccc-cccc-cccc-cccc-cccccccccccc"

	id, err := importMasterWith(ctx, ins, Master{ID: want, Src: "stream://fixed", Title: "fixed"})
	if err != nil {
		t.Fatal(err)
	}
	if id != want {
		t.Fatalf("provided id %s want %s", id, want)
	}
	row := ins.rows[db.ParseTenant("").String()+"|stream://fixed"]
	if row == nil || row.ID.String() != want {
		t.Fatalf("inserted id %+v", row)
	}
	if row.ArchivedBy.String() != want {
		t.Fatalf("empty actor should fall back to video id, archived_by %s", row.ArchivedBy.String())
	}

	actor := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	acted := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	actedID, err := importMasterWith(ctx, ins, Master{ID: acted, ActorID: actor, Src: "stream://acted", Title: "acted"})
	if err != nil {
		t.Fatal(err)
	}
	if actedID != acted {
		t.Fatalf("acted id %s", actedID)
	}
	actedRow := ins.rows[db.ParseTenant("").String()+"|stream://acted"]
	if actedRow == nil || actedRow.ArchivedBy.String() != actor {
		t.Fatalf("actor archived_by %+v", actedRow)
	}

	genA, err := importMasterWith(ctx, ins, Master{Src: "stream://generated-a", Title: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(genA); err != nil {
		t.Fatal(err)
	}
	if genA == want {
		t.Fatal("empty id reused the provided id")
	}
	genB, err := importMasterWith(ctx, ins, Master{Src: "stream://generated-b", Title: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if genA == genB {
		t.Fatal("empty id did not generate a new id")
	}
	genRow := ins.rows[db.ParseTenant("").String()+"|stream://generated-a"]
	if genRow == nil || genRow.ID.String() != genA || genRow.ArchivedBy.String() != genA {
		t.Fatalf("generated row %+v id %s", genRow, genA)
	}

	if _, err := importMasterWith(ctx, ins, Master{ID: "not-a-uuid", Src: "stream://bad"}); err == nil {
		t.Fatal("invalid id should fail")
	}
}
