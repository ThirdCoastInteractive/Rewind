// Package archive lets a Live plugin drop a master into the Rewind library
// without importing internal/db (so private modules can call it).
package archive

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
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
	// ID is videos.id when it is a valid UUID. Empty keeps uuid.New().
	ID string
}

var (
	mu        sync.RWMutex
	pg        *db.DatabaseConnection
	afterBind func()
)

// AfterBind registers fn to run once Bind has stored the database.
// Rewind Live starts the Workers AI loops from here. fn runs without the
// archive lock held.
func AfterBind(fn func()) {
	mu.Lock()
	defer mu.Unlock()
	afterBind = fn
}

// Bind is called from rewindapp after the database is open.
func Bind(dbc *db.DatabaseConnection) {
	mu.Lock()
	pg = dbc
	fn := afterBind
	mu.Unlock()
	if fn != nil {
		fn()
	}
}

// ImportMaster inserts a videos row for a Blob-backed master.
func ImportMaster(ctx context.Context, in Master) (string, error) {
	dbc, err := boundDB()
	if err != nil {
		return "", err
	}
	id, err := importMasterWith(ctx, dbc.Queries(ctx), in)
	if err != nil {
		return "", err
	}
	if err := plugin.Transcribe(ctx, id); err != nil {
		slog.Warn("archive: transcribe enqueue failed", "video_id", id, "error", err)
	}
	u, err := uuid.Parse(id)
	if err != nil {
		slog.Warn("archive: asset regeneration enqueue skipped", "video_id", id, "error", err)
		return id, nil
	}
	var idpg pgtype.UUID
	copy(idpg.Bytes[:], u[:])
	idpg.Valid = true
	if _, err := dbc.Queries(ctx).EnqueueAssetRegenerationJob(ctx, &db.EnqueueAssetRegenerationJobParams{VideoID: idpg}); err != nil {
		slog.Warn("archive: asset regeneration enqueue failed", "video_id", id, "error", err)
	}
	return id, nil
}

type masterInserter interface {
	InsertVideo(context.Context, *db.InsertVideoParams) (*db.Video, error)
	SetVideoTenantID(context.Context, pgtype.UUID, pgtype.UUID) error
}

func boundDB() (*db.DatabaseConnection, error) {
	mu.RLock()
	defer mu.RUnlock()
	if pg == nil {
		return nil, fmt.Errorf("archive: database not bound")
	}
	return pg, nil
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	var out pgtype.UUID
	copy(out.Bytes[:], id[:])
	out.Valid = true
	return out
}

func parsePGUUID(id string) (pgtype.UUID, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("archive: id: %w", err)
	}
	return pgUUID(u), nil
}

func importMasterWith(ctx context.Context, q masterInserter, in Master) (string, error) {
	var id uuid.UUID
	if in.ID == "" {
		id = uuid.New()
	} else {
		parsed, err := uuid.Parse(in.ID)
		if err != nil {
			return "", fmt.Errorf("archive: master id: %w", err)
		}
		id = parsed
	}
	idpg := pgUUID(id)
	var actor pgtype.UUID
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
	row, err := q.InsertVideo(ctx, &db.InsertVideoParams{
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
		TenantID:   db.ParseTenant(in.TenantID),
	})
	if err != nil {
		return "", err
	}
	// Stamp tenant on the persisted row (ON CONFLICT returns that id, not the pre-insert UUID).
	if err := q.SetVideoTenantID(ctx, row.ID, db.ParseTenant(in.TenantID)); err != nil {
		return "", err
	}
	return row.ID.String(), nil
}
