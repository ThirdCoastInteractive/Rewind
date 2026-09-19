-- +goose Up

-- Stable commenter identity: prefer nonempty author_id, else YouTube UC… from author_url.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION comment_author_id(author_id text, author_url text) RETURNS text
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT COALESCE(
        NULLIF(btrim(COALESCE(author_id, '')), ''),
        NULLIF(substring(COALESCE(author_url, '') from '(?i)(?:www\.)?youtube\.com/channel/(UC[A-Za-z0-9_-]+)'), '')
    );
$$;
-- +goose StatementEnd

CREATE TABLE commenters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source TEXT NOT NULL,
    author_id TEXT NOT NULL,
    author_url TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    comment_count BIGINT NOT NULL DEFAULT 0,
    channel_id UUID REFERENCES channels(id) ON DELETE SET NULL,
    style_features JSONB NOT NULL DEFAULT '{}'::jsonb,
    style_n INT NOT NULL DEFAULT 0,
    simhash BIGINT,
    UNIQUE (source, author_id),
    CHECK (btrim(author_id) <> '')
);
CREATE INDEX commenters_channel_id_idx ON commenters (channel_id) WHERE channel_id IS NOT NULL;
CREATE INDEX commenters_display_name_trgm ON commenters USING GIN (display_name gin_trgm_ops);

CREATE TABLE commenter_names (
    commenter_id UUID NOT NULL REFERENCES commenters(id) ON DELETE CASCADE,
    display_name TEXT NOT NULL,
    first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    n BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (commenter_id, display_name)
);

ALTER TABLE video_comments
    ADD COLUMN commenter_id UUID REFERENCES commenters(id) ON DELETE SET NULL;
CREATE INDEX video_comments_commenter_id_idx
    ON video_comments (commenter_id) WHERE commenter_id IS NOT NULL;
CREATE INDEX video_comments_source_author_id_idx
    ON video_comments (source, author_id)
    WHERE author_id IS NOT NULL AND btrim(author_id) <> '';
CREATE INDEX video_comments_needs_commenter_idx
    ON video_comments (video_id)
    WHERE commenter_id IS NULL AND author_id IS NOT NULL AND btrim(author_id) <> '';

CREATE TABLE comment_scores (
    comment_id UUID PRIMARY KEY REFERENCES video_comments(id) ON DELETE CASCADE,
    model_digest TEXT NOT NULL DEFAULT '',
    sentiment DOUBLE PRECISION,
    toxicity DOUBLE PRECISION,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    simhash BIGINT,
    scored_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE speech_scores (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    set_id UUID REFERENCES context_window_sets(id) ON DELETE SET NULL,
    start_ts DOUBLE PRECISION NOT NULL,
    end_ts DOUBLE PRECISION NOT NULL,
    model_digest TEXT NOT NULL DEFAULT '',
    sentiment DOUBLE PRECISION,
    toxicity DOUBLE PRECISION,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    scored_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX speech_scores_video_idx ON speech_scores (video_id, start_ts);

CREATE TABLE commenter_watchlist (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    commenter_id UUID NOT NULL REFERENCES commenters(id) ON DELETE CASCADE,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, commenter_id)
);

CREATE TABLE campaigns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL,
    simhash BIGINT,
    normalized_text TEXT NOT NULL DEFAULT '',
    first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    comment_count BIGINT NOT NULL DEFAULT 0,
    commenter_count BIGINT NOT NULL DEFAULT 0,
    video_count BIGINT NOT NULL DEFAULT 0,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (kind IN ('copypaste', 'raid'))
);
CREATE UNIQUE INDEX campaigns_copypaste_simhash_uidx
    ON campaigns (simhash)
    WHERE kind = 'copypaste' AND simhash IS NOT NULL;

CREATE TABLE campaign_members (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    comment_id UUID NOT NULL REFERENCES video_comments(id) ON DELETE CASCADE,
    commenter_id UUID REFERENCES commenters(id) ON DELETE SET NULL,
    PRIMARY KEY (campaign_id, comment_id)
);
CREATE INDEX campaign_members_comment_id_idx ON campaign_members (comment_id);

CREATE TABLE osint_flags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL,
    commenter_id UUID REFERENCES commenters(id) ON DELETE CASCADE,
    video_id UUID REFERENCES videos(id) ON DELETE CASCADE,
    campaign_id UUID REFERENCES campaigns(id) ON DELETE CASCADE,
    score DOUBLE PRECISION NOT NULL DEFAULT 0,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    dismissed_at TIMESTAMPTZ,
    dismissed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    CHECK (kind IN ('campaign', 'raid', 'newcomer', 'sock_suggest', 'toxicity_burst'))
);
CREATE UNIQUE INDEX osint_flags_open_natural_uidx
    ON osint_flags (
        kind,
        COALESCE(commenter_id::text, ''),
        COALESCE(video_id::text, ''),
        COALESCE(campaign_id::text, '')
    )
    WHERE dismissed_at IS NULL;
CREATE INDEX osint_flags_open_idx ON osint_flags (created_at DESC) WHERE dismissed_at IS NULL;

CREATE TABLE commenter_links (
    a_id UUID NOT NULL REFERENCES commenters(id) ON DELETE CASCADE,
    b_id UUID NOT NULL REFERENCES commenters(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    score DOUBLE PRECISION NOT NULL DEFAULT 0,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (a_id, b_id, kind),
    CHECK (kind IN ('style', 'user')),
    CHECK (a_id < b_id)
);

CREATE TABLE commenter_edges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_channel_id UUID REFERENCES channels(id) ON DELETE CASCADE,
    commenter_id UUID NOT NULL REFERENCES commenters(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    weight DOUBLE PRECISION NOT NULL DEFAULT 1,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    video_id UUID REFERENCES videos(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (kind IN ('commented_by', 'campaign', 'style'))
);
CREATE UNIQUE INDEX commenter_edges_identity_uidx
    ON commenter_edges (COALESCE(from_channel_id::text, ''), commenter_id, kind);
CREATE INDEX commenter_edges_commenter_idx ON commenter_edges (commenter_id);
CREATE INDEX commenter_edges_from_channel_idx
    ON commenter_edges (from_channel_id) WHERE from_channel_id IS NOT NULL;

-- +goose Down

DROP TABLE IF EXISTS commenter_edges;
DROP TABLE IF EXISTS commenter_links;
DROP TABLE IF EXISTS osint_flags;
DROP TABLE IF EXISTS campaign_members;
DROP TABLE IF EXISTS campaigns;
DROP TABLE IF EXISTS commenter_watchlist;
DROP TABLE IF EXISTS speech_scores;
DROP TABLE IF EXISTS comment_scores;

DROP INDEX IF EXISTS video_comments_needs_commenter_idx;
DROP INDEX IF EXISTS video_comments_source_author_id_idx;
DROP INDEX IF EXISTS video_comments_commenter_id_idx;
ALTER TABLE video_comments DROP COLUMN IF EXISTS commenter_id;

DROP TABLE IF EXISTS commenter_names;
DROP TABLE IF EXISTS commenters;

DROP FUNCTION IF EXISTS comment_author_id(text, text);
