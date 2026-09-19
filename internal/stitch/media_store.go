package stitch

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Asset describes an immutable project-owned editor asset.
type Asset struct {
	ID        pgtype.UUID `json:"id"`
	ProjectID pgtype.UUID `json:"project_id"`
	Hash      string      `json:"hash"`
	Path      string      `json:"-"`
	MIME      string      `json:"mime"`
	Width     int         `json:"width"`
	Height    int         `json:"height"`
	Size      int64       `json:"size"`
}

// AddAsset validates, atomically writes, and records an immutable project asset.
func (s *Store) AddAsset(ctx context.Context, owner, project pgtype.UUID, r io.Reader, _ string, exportsDir string) (Asset, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer tx.Rollback(ctx)
	var exists pgtype.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM stitch_projects WHERE id=$1 AND created_by=$2 FOR UPDATE`, project, owner).Scan(&exists); err != nil {
		if err == pgx.ErrNoRows {
			return Asset{}, ErrNotFound
		}
		return Asset{}, err
	}
	b, format, hash, err := ValidateAsset(r)
	if err != nil {
		return Asset{}, err
	}
	mime := "image/" + format
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(b))
	exportsDir, err = filepath.Abs(exportsDir)
	if err != nil {
		return Asset{}, err
	}
	dir := filepath.Join(exportsDir, "stitch-assets", project.String())
	if err = os.MkdirAll(dir, 0755); err != nil {
		return Asset{}, err
	}
	ext := format
	if ext == "jpeg" {
		ext = "jpg"
	}
	path := filepath.Join(dir, hash+"."+ext)
	tmp, err := os.CreateTemp(dir, ".asset-")
	if err != nil {
		return Asset{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_, writeErr := tmp.Write(b)
	closeErr := tmp.Close()
	if writeErr != nil {
		err = writeErr
	} else {
		err = closeErr
	}
	if err != nil {
		return Asset{}, err
	}
	if existing, readErr := os.ReadFile(path); readErr == nil {
		_, _, existingHash, _ := ValidateAsset(bytes.NewReader(existing))
		if existingHash != hash {
			return Asset{}, fmt.Errorf("asset hash collision")
		}
		_ = os.Remove(tmpName)
	} else if !os.IsNotExist(readErr) {
		return Asset{}, readErr
	} else if err = os.Rename(tmpName, path); err != nil {
		return Asset{}, err
	}
	var a Asset
	err = tx.QueryRow(ctx, `INSERT INTO stitch_assets(project_id,owner_id,hash,path,mime,width,height,size) SELECT $1,$2,$3,$4,$5,$6,$7,$8 WHERE EXISTS (SELECT 1 FROM stitch_projects WHERE id=$1 AND created_by=$2) ON CONFLICT(project_id,hash) DO UPDATE SET hash=EXCLUDED.hash RETURNING id,project_id,hash,path,mime,width,height,size`, project, owner, hash, path, mime, cfg.Width, cfg.Height, int64(len(b))).Scan(&a.ID, &a.ProjectID, &a.Hash, &a.Path, &a.MIME, &a.Width, &a.Height, &a.Size)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Asset{}, ErrNotFound
		}
		return Asset{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Asset{}, err
	}
	return a, nil
}

// GetAsset returns an asset only when it belongs to the requesting project and owner.
func (s *Store) GetAsset(ctx context.Context, owner, project, id pgtype.UUID) (Asset, error) {
	var a Asset
	err := s.db.QueryRow(ctx, `SELECT a.id,a.project_id,a.hash,a.path,a.mime,a.width,a.height,a.size FROM stitch_assets a JOIN stitch_projects p ON p.id=a.project_id WHERE a.id=$1 AND a.project_id=$2`, id, project).Scan(&a.ID, &a.ProjectID, &a.Hash, &a.Path, &a.MIME, &a.Width, &a.Height, &a.Size)
	if err == pgx.ErrNoRows {
		return Asset{}, ErrNotFound
	}
	if err != nil {
		return Asset{}, err
	}
	return a, nil
}
