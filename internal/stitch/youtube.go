package stitch

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
)

const MaxYouTubeDescription = 5000
const MaxYouTubeTags = 30
const MaxYouTubeTagLen = 100

// NormalizeYouTube trims and validates upload description/tags for YouTube Studio.
func NormalizeYouTube(description string, tags []string) (string, []string, error) {
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > MaxYouTubeDescription {
		return "", nil, fmt.Errorf("youtube description exceeds %d characters", MaxYouTubeDescription)
	}
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		n := utf8.RuneCountInString(tag)
		if n > MaxYouTubeTagLen {
			return "", nil, fmt.Errorf("youtube tag exceeds %d characters", MaxYouTubeTagLen)
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	if len(out) > MaxYouTubeTags {
		return "", nil, fmt.Errorf("youtube tags exceed %d entries", MaxYouTubeTags)
	}
	return description, out, nil
}

// SetYouTube stores project upload metadata without bumping document revision.
func (s *Store) SetYouTube(ctx context.Context, owner, project pgtype.UUID, description string, tags []string) (Snapshot, error) {
	description, tags, err := NormalizeYouTube(description, tags)
	if err != nil {
		return Snapshot{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx, `UPDATE stitch_projects SET description=$3, tags=$4, updated_at=NOW() WHERE id=$1 AND created_by=$2`, project, owner, description, tags)
	if err != nil {
		return Snapshot{}, err
	}
	if ct.RowsAffected() == 0 {
		return Snapshot{}, ErrNotFound
	}
	if _, err = tx.Exec(ctx, `SELECT pg_notify('stitch_projects_changed', $1)`, uuidText(project)); err != nil {
		return Snapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Snapshot{}, err
	}
	return s.Get(ctx, owner, project)
}
