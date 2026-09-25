-- name: UpsertSpeakerTurns :exec
INSERT INTO speaker_turns (video_id, model, fingerprint, turns)
VALUES (sqlc.arg(video_id), sqlc.arg(model), sqlc.arg(fingerprint), sqlc.arg(turns))
ON CONFLICT (video_id) DO UPDATE SET
    model = EXCLUDED.model,
    fingerprint = EXCLUDED.fingerprint,
    turns = EXCLUDED.turns,
    updated_at = now();

-- name: GetSpeakerTurns :one
SELECT video_id, model, fingerprint, turns, created_at, updated_at
FROM speaker_turns
WHERE video_id = sqlc.arg(video_id);

-- name: ListVideosNeedingDiarize :many
SELECT t.video_id
FROM video_transcripts t
WHERE t.text <> ''
  AND NOT EXISTS (
      SELECT 1 FROM speaker_turns s
      WHERE s.video_id = t.video_id
        AND s.fingerprint = sqlc.arg(fingerprint)
  )
GROUP BY t.video_id
ORDER BY max(t.updated_at) DESC
LIMIT sqlc.arg(row_limit);
