-- OSINT detector queries. Implemented via pgx in internal/osint; this file
-- documents the statements. Natural-key idempotency uses osint_flags.natural_key.

-- query: UpsertOsintFlag :exec
INSERT INTO osint_flags (kind, commenter_id, video_id, campaign_id, score, evidence, natural_key)
VALUES (
  sqlc.arg(kind),
  sqlc.narg(commenter_id),
  sqlc.narg(video_id),
  sqlc.narg(campaign_id),
  sqlc.arg(score),
  sqlc.arg(evidence)::jsonb,
  sqlc.arg(natural_key)
)
ON CONFLICT (natural_key) WHERE natural_key IS NOT NULL DO NOTHING;

-- query: UpsertCopypasteCampaign :one
INSERT INTO campaigns (kind, simhash, normalized_text, first_seen, last_seen, comment_count, commenter_count, video_count, evidence)
VALUES (
  'copypaste',
  sqlc.arg(simhash),
  sqlc.arg(normalized_text),
  sqlc.arg(first_seen),
  sqlc.arg(last_seen),
  sqlc.arg(comment_count),
  sqlc.arg(commenter_count),
  sqlc.arg(video_count),
  sqlc.arg(evidence)::jsonb
)
ON CONFLICT (kind, simhash) DO UPDATE SET
  last_seen = GREATEST(campaigns.last_seen, EXCLUDED.last_seen),
  first_seen = LEAST(campaigns.first_seen, EXCLUDED.first_seen),
  comment_count = GREATEST(campaigns.comment_count, EXCLUDED.comment_count),
  commenter_count = GREATEST(campaigns.commenter_count, EXCLUDED.commenter_count),
  video_count = GREATEST(campaigns.video_count, EXCLUDED.video_count),
  evidence = EXCLUDED.evidence,
  normalized_text = EXCLUDED.normalized_text
RETURNING id;

-- query: InsertCampaignMember :exec
INSERT INTO campaign_members (campaign_id, comment_id, commenter_id)
VALUES (sqlc.arg(campaign_id), sqlc.arg(comment_id), sqlc.arg(commenter_id))
ON CONFLICT DO NOTHING;

-- query: UpsertStyleCommenterLink :exec
INSERT INTO commenter_links (a_id, b_id, kind, score, evidence, created_by)
VALUES (sqlc.arg(a_id), sqlc.arg(b_id), 'style', sqlc.arg(score), sqlc.arg(evidence)::jsonb, NULL)
ON CONFLICT (a_id, b_id, kind) DO UPDATE SET
  score = EXCLUDED.score,
  evidence = EXCLUDED.evidence;

-- query: UpsertCommentedByEdge :exec
INSERT INTO commenter_edges (from_channel_id, commenter_id, kind, weight, evidence, video_id)
VALUES (
  sqlc.arg(from_channel_id),
  sqlc.arg(commenter_id),
  'commented_by',
  sqlc.arg(weight),
  sqlc.arg(evidence)::jsonb,
  sqlc.narg(video_id)
)
ON CONFLICT (COALESCE(from_channel_id::text, ''), commenter_id, kind) DO UPDATE SET
  weight = GREATEST(commenter_edges.weight, EXCLUDED.weight),
  evidence = EXCLUDED.evidence,
  video_id = COALESCE(EXCLUDED.video_id, commenter_edges.video_id),
  updated_at = NOW();

-- query: UpdateCommentScoreSimhash :exec
UPDATE comment_scores
SET simhash = sqlc.arg(simhash), scored_at = COALESCE(scored_at, NOW())
WHERE comment_id = sqlc.arg(comment_id) AND simhash IS NULL;

-- query: UpdateCommenterStyleFeatures :exec
UPDATE commenters
SET style_features = sqlc.arg(style_features)::jsonb,
    style_n = sqlc.arg(style_n),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- query: ListCommentsMissingSimhash :many
SELECT vc.id, vc.text
FROM comment_scores cs
JOIN video_comments vc ON vc.id = cs.comment_id
WHERE cs.simhash IS NULL AND vc.text IS NOT NULL AND length(vc.text) > 0
ORDER BY cs.scored_at DESC NULLS LAST
LIMIT sqlc.arg(row_limit);

-- query: ListRecentScoredCommentsForCampaigns :many
SELECT vc.id, vc.video_id, vc.commenter_id, vc.text, vc.published_at, cs.simhash
FROM comment_scores cs
JOIN video_comments vc ON vc.id = cs.comment_id
WHERE cs.simhash IS NOT NULL
  AND vc.commenter_id IS NOT NULL
  AND length(COALESCE(vc.text, '')) >= sqlc.arg(min_len)
ORDER BY cs.scored_at DESC NULLS LAST
LIMIT sqlc.arg(row_limit);
