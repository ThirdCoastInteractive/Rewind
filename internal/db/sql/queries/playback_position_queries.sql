-- GetPlaybackPosition retrieves the last playback position for a user/video
-- name: GetPlaybackPosition :one
SELECT position_seconds, updated_at
FROM playback_positions
WHERE user_id = sqlc.arg(user_id)
  AND video_id = sqlc.arg(video_id);

-- UpsertPlaybackPosition saves or updates the playback position for a user/video
-- name: UpsertPlaybackPosition :exec
INSERT INTO playback_positions (user_id, video_id, position_seconds, updated_at)
VALUES (sqlc.arg(user_id), sqlc.arg(video_id), sqlc.arg(position_seconds), CURRENT_TIMESTAMP)
ON CONFLICT (user_id, video_id)
DO UPDATE SET
    position_seconds = EXCLUDED.position_seconds,
    updated_at = CURRENT_TIMESTAMP;

