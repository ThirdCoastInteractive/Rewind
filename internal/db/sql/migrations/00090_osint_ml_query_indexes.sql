-- +goose NO TRANSACTION

-- +goose Up

-- Sock flags are one open row per commenter *pair*. The old unique index
-- (kind, commenter_id, video_id, campaign_id) collapsed every pair that
-- shared the lower UUID. natural_key already encodes the pair.
ALTER TABLE osint_flags ADD COLUMN IF NOT EXISTS natural_key TEXT;
UPDATE osint_flags
SET natural_key = evidence->>'natural_key'
WHERE natural_key IS NULL AND evidence ? 'natural_key';

DROP INDEX IF EXISTS osint_flags_open_natural_uidx;
CREATE UNIQUE INDEX IF NOT EXISTS osint_flags_natural_key_uidx
    ON osint_flags (natural_key)
    WHERE natural_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS osint_flags_open_raid_campaign_idx
    ON osint_flags (campaign_id)
    WHERE dismissed_at IS NULL AND kind = 'raid' AND campaign_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS campaign_members_commenter_id_idx
    ON campaign_members (commenter_id)
    WHERE commenter_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS commenters_last_seen_idx
    ON commenters (last_seen DESC NULLS LAST);
CREATE INDEX CONCURRENTLY IF NOT EXISTS commenters_style_ready_idx
    ON commenters (last_seen DESC NULLS LAST)
    WHERE style_n >= 3;

CREATE INDEX IF NOT EXISTS comment_scores_missing_simhash_idx
    ON comment_scores (scored_at DESC)
    WHERE simhash IS NULL;
CREATE INDEX IF NOT EXISTS comment_scores_scored_at_idx
    ON comment_scores (scored_at DESC);

-- 2.6M-row heap: CONCURRENTLY so ingest/ML keep writing while these build.
CREATE INDEX CONCURRENTLY IF NOT EXISTS video_comments_raid_idx
    ON video_comments (video_id, published_at)
    WHERE published_at IS NOT NULL AND commenter_id IS NOT NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS video_comments_published_commenter_idx
    ON video_comments (published_at DESC)
    WHERE published_at IS NOT NULL AND commenter_id IS NOT NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS video_comments_commenter_published_idx
    ON video_comments (commenter_id, published_at DESC)
    WHERE commenter_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS context_windows_topic_bind_idx
    ON context_windows (updated_at DESC, id)
    WHERE NOT stale AND kind = 'window' AND topic_resolved_at IS NULL;

-- ClaimMLJob filters waiting_assets/retry_wait too; the original partial
-- index only covered queued/waiting_model and was unused.
CREATE INDEX CONCURRENTLY IF NOT EXISTS ml_jobs_claim_kind_idx
    ON ml_jobs (kind, priority, created_at)
    WHERE status IN ('queued', 'waiting_model', 'waiting_assets', 'retry_wait');
CREATE INDEX CONCURRENTLY IF NOT EXISTS ml_jobs_processing_kind_idx
    ON ml_jobs (kind)
    WHERE status = 'processing';
DROP INDEX CONCURRENTLY IF EXISTS ml_jobs_claim_idx;

-- +goose Down

CREATE INDEX CONCURRENTLY IF NOT EXISTS ml_jobs_claim_idx
    ON ml_jobs (priority, created_at)
    WHERE status IN ('queued', 'waiting_model');
DROP INDEX CONCURRENTLY IF EXISTS ml_jobs_processing_kind_idx;
DROP INDEX CONCURRENTLY IF EXISTS ml_jobs_claim_kind_idx;

DROP INDEX CONCURRENTLY IF EXISTS video_comments_commenter_published_idx;
DROP INDEX CONCURRENTLY IF EXISTS video_comments_published_commenter_idx;
DROP INDEX CONCURRENTLY IF EXISTS video_comments_raid_idx;

DROP INDEX IF EXISTS context_windows_topic_bind_idx;
DROP INDEX IF EXISTS comment_scores_scored_at_idx;
DROP INDEX IF EXISTS comment_scores_missing_simhash_idx;
DROP INDEX CONCURRENTLY IF EXISTS commenters_style_ready_idx;
DROP INDEX CONCURRENTLY IF EXISTS commenters_last_seen_idx;
DROP INDEX IF EXISTS campaign_members_commenter_id_idx;
DROP INDEX IF EXISTS osint_flags_open_raid_campaign_idx;
DROP INDEX IF EXISTS osint_flags_natural_key_uidx;

CREATE UNIQUE INDEX IF NOT EXISTS osint_flags_open_natural_uidx
    ON osint_flags (
        kind,
        COALESCE(commenter_id::text, ''),
        COALESCE(video_id::text, ''),
        COALESCE(campaign_id::text, '')
    )
    WHERE dismissed_at IS NULL;

ALTER TABLE osint_flags DROP COLUMN IF EXISTS natural_key;
