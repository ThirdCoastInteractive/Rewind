-- name: UpsertPlayerScenePreset :one
INSERT INTO player_scene_presets (producer_id, name, scene)
VALUES (sqlc.arg(producer_id), sqlc.arg(name), sqlc.arg(scene))
ON CONFLICT (producer_id, name)
DO UPDATE SET scene = EXCLUDED.scene, updated_at = NOW()
RETURNING *;

-- name: ListPlayerScenePresetsByProducer :many
SELECT * FROM player_scene_presets
WHERE producer_id = sqlc.arg(producer_id)
ORDER BY updated_at DESC;

-- name: GetPlayerScenePresetByID :one
SELECT * FROM player_scene_presets
WHERE id = sqlc.arg(id);

-- name: DeletePlayerScenePreset :exec
DELETE FROM player_scene_presets
WHERE id = sqlc.arg(id) AND producer_id = sqlc.arg(producer_id);
