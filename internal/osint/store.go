package osint

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"thirdcoast.systems/rewind/internal/db"
)

type store struct {
	pool *pgxpool.Pool
}

// upsertFlag inserts a flag unless a row already shares the natural key
// (including dismissed rows, so operators are not re-prompted).
func (s store) upsertFlag(ctx context.Context, kind string, commenterID, videoID, campaignID *uuid.UUID, score float64, naturalKey string, evidence map[string]any) error {
	if evidence == nil {
		evidence = map[string]any{}
	}
	evidence["natural_key"] = naturalKey
	raw, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO osint_flags (kind, commenter_id, video_id, campaign_id, score, evidence, natural_key)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
ON CONFLICT (natural_key) WHERE natural_key IS NOT NULL DO NOTHING`,
		kind, commenterID, videoID, campaignID, score, raw, naturalKey)
	if err == nil || db.IsUniqueViolationErr(err) {
		return nil
	}
	if db.IsUndefinedColumnErr(err) || isOnConflictTargetErr(err) {
		// Pre-00090: no natural_key column or unique index yet.
		_, err = s.pool.Exec(ctx, `
INSERT INTO osint_flags (kind, commenter_id, video_id, campaign_id, score, evidence)
SELECT $1, $2, $3, $4, $5, $6::jsonb
WHERE NOT EXISTS (
  SELECT 1 FROM osint_flags WHERE evidence->>'natural_key' = $7
)`,
			kind, commenterID, videoID, campaignID, score, raw, naturalKey)
		if db.IsUniqueViolationErr(err) {
			return nil
		}
	}
	return err
}

func isOnConflictTargetErr(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P10"
}

func (s store) upsertCampaign(ctx context.Context, kind string, simhash uint64, normalized string, first, last time.Time, commentCount, commenterCount, videoCount int, evidence map[string]any) (uuid.UUID, error) {
	raw, err := json.Marshal(evidence)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
INSERT INTO campaigns (kind, simhash, normalized_text, first_seen, last_seen, comment_count, commenter_count, video_count, evidence)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
ON CONFLICT (kind, simhash) WHERE kind = 'copypaste'
DO UPDATE SET
  last_seen = GREATEST(campaigns.last_seen, EXCLUDED.last_seen),
  first_seen = LEAST(campaigns.first_seen, EXCLUDED.first_seen),
  comment_count = GREATEST(campaigns.comment_count, EXCLUDED.comment_count),
  commenter_count = GREATEST(campaigns.commenter_count, EXCLUDED.commenter_count),
  video_count = GREATEST(campaigns.video_count, EXCLUDED.video_count),
  evidence = EXCLUDED.evidence,
  normalized_text = EXCLUDED.normalized_text
RETURNING id`,
		kind, int64(simhash), normalized, first, last, commentCount, commenterCount, videoCount, raw,
	).Scan(&id)
	if err != nil {
		// Contract tables may lack the partial unique index yet; fall back to select-or-insert.
		err2 := s.pool.QueryRow(ctx, `
SELECT id FROM campaigns WHERE kind = $1 AND simhash = $2 LIMIT 1`, kind, int64(simhash)).Scan(&id)
		if err2 == nil {
			_, _ = s.pool.Exec(ctx, `
UPDATE campaigns SET
  last_seen = GREATEST(last_seen, $2),
  first_seen = LEAST(first_seen, $3),
  comment_count = GREATEST(comment_count, $4),
  commenter_count = GREATEST(commenter_count, $5),
  video_count = GREATEST(video_count, $6),
  evidence = $7::jsonb,
  normalized_text = $8
WHERE id = $1`, id, last, first, commentCount, commenterCount, videoCount, raw, normalized)
			return id, nil
		}
		err3 := s.pool.QueryRow(ctx, `
INSERT INTO campaigns (kind, simhash, normalized_text, first_seen, last_seen, comment_count, commenter_count, video_count, evidence)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
RETURNING id`,
			kind, int64(simhash), normalized, first, last, commentCount, commenterCount, videoCount, raw,
		).Scan(&id)
		return id, err3
	}
	return id, nil
}

func (s store) insertRaidCampaign(ctx context.Context, videoID uuid.UUID, bucketStart time.Time, normalized string, first, last time.Time, commentCount, commenterCount int, evidence map[string]any) (uuid.UUID, error) {
	raw, err := json.Marshal(evidence)
	if err != nil {
		return uuid.Nil, err
	}
	// Encode video+bucket into simhash slot as a stable surrogate for raid campaigns.
	surrogate := fnv64(RaidBucketKey(videoID, bucketStart.Unix()))
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
SELECT id FROM campaigns WHERE kind = 'raid' AND simhash = $1 LIMIT 1`, int64(surrogate)).Scan(&id)
	if err == nil {
		_, _ = s.pool.Exec(ctx, `
UPDATE campaigns SET
  last_seen = GREATEST(last_seen, $2),
  first_seen = LEAST(first_seen, $3),
  comment_count = GREATEST(comment_count, $4),
  commenter_count = GREATEST(commenter_count, $5),
  video_count = 1,
  evidence = $6::jsonb,
  normalized_text = $7
WHERE id = $1`, id, last, first, commentCount, commenterCount, raw, normalized)
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	err = s.pool.QueryRow(ctx, `
INSERT INTO campaigns (kind, simhash, normalized_text, first_seen, last_seen, comment_count, commenter_count, video_count, evidence)
VALUES ('raid', $1, $2, $3, $4, $5, $6, 1, $7::jsonb)
RETURNING id`, int64(surrogate), normalized, first, last, commentCount, commenterCount, raw).Scan(&id)
	return id, err
}

func (s store) addCampaignMember(ctx context.Context, campaignID, commentID, commenterID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO campaign_members (campaign_id, comment_id, commenter_id)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING`, campaignID, commentID, commenterID)
	return err
}

func (s store) upsertStyleLink(ctx context.Context, a, b uuid.UUID, score float64, evidence map[string]any) error {
	lo, hi := OrderedCommenterPair(a, b)
	raw, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
INSERT INTO commenter_links (a_id, b_id, kind, score, evidence, created_by)
VALUES ($1, $2, 'style', $3, $4::jsonb, NULL)
ON CONFLICT (a_id, b_id, kind) DO UPDATE SET
  score = EXCLUDED.score,
  evidence = EXCLUDED.evidence`, lo, hi, score, raw)
	if err == nil {
		_ = tag
		return nil
	}
	// Fallback when unique (a_id,b_id,kind) is not present yet.
	var one int
	err2 := s.pool.QueryRow(ctx, `
SELECT 1 FROM commenter_links WHERE a_id = $1 AND b_id = $2 AND kind = 'style' LIMIT 1`, lo, hi).Scan(&one)
	if err2 == nil {
		_, err = s.pool.Exec(ctx, `
UPDATE commenter_links SET score = $3, evidence = $4::jsonb
WHERE a_id = $1 AND b_id = $2 AND kind = 'style'`, lo, hi, score, raw)
		return err
	}
	if !errors.Is(err2, pgx.ErrNoRows) {
		return err
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO commenter_links (a_id, b_id, kind, score, evidence, created_by)
VALUES ($1, $2, 'style', $3, $4::jsonb, NULL)`, lo, hi, score, raw)
	return err
}

func (s store) userLinkSet(ctx context.Context) (map[[2]uuid.UUID]struct{}, error) {
	rows, err := s.pool.Query(ctx, `
SELECT a_id, b_id FROM commenter_links WHERE kind = 'user'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[[2]uuid.UUID]struct{}{}
	for rows.Next() {
		var a, b uuid.UUID
		if err := rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		lo, hi := OrderedCommenterPair(a, b)
		out[[2]uuid.UUID{lo, hi}] = struct{}{}
	}
	return out, rows.Err()
}

func (s store) upsertCommentedByEdge(ctx context.Context, channelID, commenterID uuid.UUID, weight float64, videoID *uuid.UUID, evidence map[string]any) error {
	raw, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	// Unique index is commenter_edges_identity_uidx
	// (COALESCE(from_channel_id::text, ''), commenter_id, kind) — column
	// inference on (from_channel_id, commenter_id, kind) does not match it.
	_, err = s.pool.Exec(ctx, `
INSERT INTO commenter_edges (from_channel_id, commenter_id, kind, weight, evidence, video_id)
VALUES ($1, $2, 'commented_by', $3, $4::jsonb, $5)
ON CONFLICT (COALESCE(from_channel_id::text, ''), commenter_id, kind) DO UPDATE SET
  weight = GREATEST(commenter_edges.weight, EXCLUDED.weight),
  evidence = EXCLUDED.evidence,
  video_id = COALESCE(EXCLUDED.video_id, commenter_edges.video_id),
  updated_at = NOW()`,
		channelID, commenterID, weight, raw, videoID)
	return err
}

func (s store) updateCommentSimhash(ctx context.Context, commentID uuid.UUID, hash uint64) error {
	_, err := s.pool.Exec(ctx, `
UPDATE comment_scores SET simhash = $2, scored_at = COALESCE(scored_at, NOW())
WHERE comment_id = $1 AND simhash IS NULL`, commentID, int64(hash))
	return err
}

func (s store) updateCommenterStyle(ctx context.Context, commenterID uuid.UUID, features map[string]float64, styleN int, hash *uint64) error {
	raw, err := json.Marshal(features)
	if err != nil {
		return err
	}
	if hash != nil {
		_, err = s.pool.Exec(ctx, `
UPDATE commenters SET style_features = $2::jsonb, style_n = $3, simhash = $4, updated_at = NOW()
WHERE id = $1`, commenterID, raw, styleN, int64(*hash))
		return err
	}
	_, err = s.pool.Exec(ctx, `
UPDATE commenters SET style_features = $2::jsonb, style_n = $3, updated_at = NOW()
WHERE id = $1`, commenterID, raw, styleN)
	return err
}
