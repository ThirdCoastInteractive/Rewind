package textcls

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Contract tables (created by migration 00087_osint_desk in another leaf):
//
// CREATE TABLE comment_scores (
//     comment_id UUID PRIMARY KEY REFERENCES video_comments(id) ON DELETE CASCADE,
//     model_digest TEXT NOT NULL DEFAULT '',
//     sentiment DOUBLE PRECISION,
//     toxicity DOUBLE PRECISION,
//     labels JSONB NOT NULL DEFAULT '{}'::jsonb,
//     simhash BIGINT,
//     scored_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
// );
//
// CREATE TABLE speech_scores (
//     id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
//     video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
//     set_id UUID REFERENCES context_window_sets(id) ON DELETE SET NULL,
//     start_ts DOUBLE PRECISION NOT NULL,
//     end_ts DOUBLE PRECISION NOT NULL,
//     model_digest TEXT NOT NULL DEFAULT '',
//     sentiment DOUBLE PRECISION,
//     toxicity DOUBLE PRECISION,
//     labels JSONB NOT NULL DEFAULT '{}'::jsonb,
//     scored_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
// );

// UnscoredComment is a video_comments row needing classification.
type UnscoredComment struct {
	ID   pgtype.UUID
	Text string
}

// SpeechWindow is a span of transcript/context text to score.
type SpeechWindow struct {
	SetID   pgtype.UUID
	StartTS float64
	EndTS   float64
	Text    string
}

// CommentBatchHashForVideo is sha256(max(updated_at)|count) for enqueue uniqueness.
func CommentBatchHashForVideo(ctx context.Context, pool *pgxpool.Pool, videoID pgtype.UUID) (string, error) {
	var maxUpdated *time.Time
	var count int64
	err := pool.QueryRow(ctx, `
SELECT MAX(updated_at), COUNT(*)::bigint
FROM video_comments
WHERE video_id = $1
`, videoID).Scan(&maxUpdated, &count)
	if err != nil {
		return "", err
	}
	if count == 0 {
		return "", pgx.ErrNoRows
	}
	stamp := ""
	if maxUpdated != nil {
		stamp = maxUpdated.UTC().Format(time.RFC3339Nano)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", stamp, count)))
	return hex.EncodeToString(sum[:]), nil
}

// ListUnscoredCommentsForVideo returns comments missing a score for model_digest.
func ListUnscoredCommentsForVideo(ctx context.Context, pool *pgxpool.Pool, videoID pgtype.UUID, modelDigest string) ([]UnscoredComment, error) {
	rows, err := pool.Query(ctx, `
SELECT c.id, COALESCE(c.text, '')
FROM video_comments c
LEFT JOIN comment_scores s ON s.comment_id = c.id
WHERE c.video_id = $1
  AND (s.comment_id IS NULL OR s.model_digest IS DISTINCT FROM $2)
ORDER BY c.created_at
`, videoID, modelDigest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UnscoredComment
	for rows.Next() {
		var row UnscoredComment
		if err = rows.Scan(&row.ID, &row.Text); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpsertCommentScore writes one comment_scores row.
func UpsertCommentScore(ctx context.Context, pool *pgxpool.Pool, commentID pgtype.UUID, modelDigest string, sentiment, toxicity float64, labels []byte) error {
	if labels == nil {
		labels = []byte("{}")
	}
	_, err := pool.Exec(ctx, `
INSERT INTO comment_scores(comment_id, model_digest, sentiment, toxicity, labels, scored_at)
VALUES($1, $2, $3, $4, $5::jsonb, now())
ON CONFLICT (comment_id) DO UPDATE SET
    model_digest = EXCLUDED.model_digest,
    sentiment = EXCLUDED.sentiment,
    toxicity = EXCLUDED.toxicity,
    labels = EXCLUDED.labels,
    scored_at = now()
`, commentID, modelDigest, sentiment, toxicity, labels)
	return err
}

// ListVideosNeedingCommentClassify finds videos with comments lacking current digest scores.
func ListVideosNeedingCommentClassify(ctx context.Context, pool *pgxpool.Pool, modelDigest string, limit int) ([]pgtype.UUID, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
SELECT DISTINCT c.video_id
FROM video_comments c
LEFT JOIN comment_scores s ON s.comment_id = c.id
WHERE s.comment_id IS NULL OR s.model_digest IS DISTINCT FROM $1
ORDER BY c.video_id
LIMIT $2
`, modelDigest, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListContextWindowsForSpeechTone prefers succeeded context windows; caller falls back to cues.
func ListContextWindowsForSpeechTone(ctx context.Context, pool *pgxpool.Pool, videoID pgtype.UUID) ([]SpeechWindow, error) {
	rows, err := pool.Query(ctx, `
SELECT cw.set_id, cw.start_ts, cw.end_ts,
       trim(both FROM concat_ws(' ', COALESCE(cw.title, ''), COALESCE(cw.summary, ''), COALESCE(cw.transcript_cue_evidence::text, '')))
FROM context_windows cw
JOIN context_window_sets s ON s.id = cw.set_id
WHERE cw.video_id = $1
  AND s.status = 'succeeded'
  AND NOT cw.stale
ORDER BY cw.start_ts, cw.end_ts
`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpeechWindow
	for rows.Next() {
		var w SpeechWindow
		if err = rows.Scan(&w.SetID, &w.StartTS, &w.EndTS, &w.Text); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// UpsertSpeechScore inserts or refreshes a speech span score.
func UpsertSpeechScore(ctx context.Context, pool *pgxpool.Pool, videoID, setID pgtype.UUID, startTS, endTS float64, modelDigest string, sentiment, toxicity float64, labels []byte) error {
	if labels == nil {
		labels = []byte("{}")
	}
	var set any
	if setID.Valid {
		set = setID
	}
	_, err := pool.Exec(ctx, `
INSERT INTO speech_scores(video_id, set_id, start_ts, end_ts, model_digest, sentiment, toxicity, labels, scored_at)
VALUES($1, $2, $3, $4, $5, $6, $7, $8::jsonb, now())
`, videoID, set, startTS, endTS, modelDigest, sentiment, toxicity, labels)
	return err
}

// ListVideosNeedingSpeechTone finds videos with transcripts but no speech_scores for digest.
func ListVideosNeedingSpeechTone(ctx context.Context, pool *pgxpool.Pool, modelDigest string, limit int) ([]pgtype.UUID, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
SELECT t.video_id
FROM video_transcripts t
WHERE NOT EXISTS (
    SELECT 1 FROM speech_scores s
    WHERE s.video_id = t.video_id AND s.model_digest = $1
)
ORDER BY t.video_id
LIMIT $2
`, modelDigest, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// LabelsJSON marshals classifier label maps.
func LabelsJSON(labels map[string]any) []byte {
	if labels == nil {
		return []byte("{}")
	}
	raw, err := json.Marshal(labels)
	if err != nil {
		return []byte("{}")
	}
	return raw
}
