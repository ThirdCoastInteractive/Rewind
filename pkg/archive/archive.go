// Package archive lets a Live plugin drop a master into the Rewind library
// without importing internal/db (so private modules can call it).
package archive

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/videoinfo"
)

// Master is a finished recording sitting on the Blob plugin.
type Master struct {
	TenantID string
	Title    string
	Src      string // unique, e.g. stream://{input}/{vod}
	BlobKey  string
	FileSize int64
	ActorID  string
}

var (
	mu sync.RWMutex
	pg *db.DatabaseConnection
)

// Bind is called from rewindapp after the database is open.
func Bind(dbc *db.DatabaseConnection) {
	mu.Lock()
	defer mu.Unlock()
	pg = dbc
}

// ImportMaster inserts a videos row for a Blob-backed master.
func ImportMaster(ctx context.Context, in Master) (string, error) {
	mu.RLock()
	dbc := pg
	mu.RUnlock()
	if dbc == nil {
		return "", fmt.Errorf("archive: database not bound")
	}
	id := uuid.New()
	var idpg, actor pgtype.UUID
	copy(idpg.Bytes[:], id[:])
	idpg.Valid = true
	if in.ActorID != "" {
		if u, err := uuid.Parse(in.ActorID); err == nil {
			copy(actor.Bytes[:], u[:])
			actor.Valid = true
		}
	}
	if !actor.Valid {
		actor = idpg
	}
	path := in.BlobKey
	size := in.FileSize
	title := in.Title
	if title == "" {
		title = "Live recording"
	}
	src := in.Src
	if src == "" {
		src = "stream://" + id.String()
	}
	row, err := dbc.Queries(ctx).InsertVideo(ctx, &db.InsertVideoParams{
		ID:         idpg,
		Src:        src,
		ArchivedBy: actor,
		Title:      title,
		Tags:       []string{},
		Uploader:   "live",
		Info:       videoinfo.VideoInfo{},
		Comments:   []byte("[]"),
		VideoPath:  &path,
		FileSize:   &size,
		Media:      "file",
	})
	if err != nil {
		return "", err
	}
	if in.TenantID != "" {
		if _, err := dbc.Exec(ctx, `UPDATE videos SET tenant_id = $1 WHERE id = $2`, in.TenantID, id); err != nil {
			return "", fmt.Errorf("set tenant_id: %w", err)
		}
	}
	return row.ID.String(), nil
}
