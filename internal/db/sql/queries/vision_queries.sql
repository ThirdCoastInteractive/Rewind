-- name: RegisterEmbeddingModel :exec
INSERT INTO embedding_models(id,name,revision,recipe,dimensions,license) VALUES(sqlc.arg(id),sqlc.arg(name),sqlc.arg(revision),sqlc.arg(recipe),512,sqlc.arg(license)) ON CONFLICT DO NOTHING;

-- name: ListVisualCandidates :many
SELECT v.* FROM videos v LEFT JOIN vision_asset_checks a ON a.video_id=v.id AND a.kind='visual_index'
WHERE v.media='file' AND v.duration_seconds>0 AND (a.checked_at IS NULL OR a.checked_at<now()-interval '1 hour')
ORDER BY a.checked_at NULLS FIRST,v.created_at DESC LIMIT 50;

-- name: ListFaceCandidates :many
SELECT v.* FROM videos v LEFT JOIN vision_asset_checks a ON a.video_id=v.id AND a.kind='face_index' WHERE v.media='file' AND v.duration_seconds>0
AND EXISTS(SELECT 1 FROM face_index_selections s WHERE s.enabled AND (s.video_id=v.id OR s.channel_id=v.channel_row_id))
AND (a.checked_at IS NULL OR a.checked_at<now()-interval '1 hour')
ORDER BY a.checked_at NULLS FIRST,v.created_at DESC LIMIT 50;

-- name: RecordVisionAssetCheck :exec
INSERT INTO vision_asset_checks(video_id,kind) VALUES(sqlc.arg(video_id),sqlc.arg(kind)) ON CONFLICT(video_id,kind) DO UPDATE SET checked_at=now();

-- name: ResumeSelectedFaceJobs :exec
UPDATE ml_jobs j SET status='queued',retry_at=now() FROM videos v WHERE j.video_id=v.id AND j.kind='face_index' AND j.status='paused'
AND EXISTS(SELECT 1 FROM face_index_selections s WHERE s.enabled AND (s.video_id=v.id OR s.channel_id=v.channel_row_id));

-- name: FaceIndexEnabled :one
SELECT EXISTS(SELECT 1 FROM face_index_selections s JOIN videos v ON s.video_id=v.id OR s.channel_id=v.channel_row_id WHERE s.enabled AND v.id=sqlc.arg(video_id))::boolean;

-- name: SelectFaceVideo :exec
INSERT INTO face_index_selections(video_id,enabled) VALUES(sqlc.arg(video_id),sqlc.arg(enabled)) ON CONFLICT(video_id) DO UPDATE SET enabled=EXCLUDED.enabled;

-- name: SelectFaceChannel :exec
INSERT INTO face_index_selections(channel_id,enabled) VALUES(sqlc.arg(channel_id),sqlc.arg(enabled)) ON CONFLICT(channel_id) DO UPDATE SET enabled=EXCLUDED.enabled;

-- name: EnsureVisualIndexSet :one
INSERT INTO visual_index_sets(video_id,model_id,asset_fingerprint,interval_seconds,start_ts,end_ts) VALUES(sqlc.arg(video_id),sqlc.arg(model_id),sqlc.arg(asset_fingerprint),sqlc.arg(interval_seconds),sqlc.arg(start_ts),sqlc.arg(end_ts))
ON CONFLICT(video_id,model_id,asset_fingerprint,interval_seconds,start_ts,end_ts) DO UPDATE SET updated_at=now() RETURNING *;

-- name: EnsureFaceIndexSet :one
INSERT INTO face_index_sets(video_id,model_id,asset_fingerprint,interval_seconds,start_ts,end_ts) VALUES(sqlc.arg(video_id),sqlc.arg(model_id),sqlc.arg(asset_fingerprint),sqlc.arg(interval_seconds),sqlc.arg(start_ts),sqlc.arg(end_ts))
ON CONFLICT(video_id,model_id,asset_fingerprint,interval_seconds,start_ts,end_ts) DO UPDATE SET updated_at=now() RETURNING *;

-- name: SaveVisualEmbedding :exec
INSERT INTO visual_frame_embeddings(set_id,sample_index,sample_ts,frame_ref,embedding) VALUES(sqlc.arg(set_id),sqlc.arg(sample_index),sqlc.arg(sample_ts),sqlc.arg(frame_ref),sqlc.arg(embedding)::text::vector) ON CONFLICT DO NOTHING;

-- name: SaveFaceObservation :exec
INSERT INTO face_observations(set_id,sample_index,face_index,sample_ts,frame_ref,width,height,box,score,eligible,embedding)
VALUES(sqlc.arg(set_id),sqlc.arg(sample_index),sqlc.arg(face_index),sqlc.arg(sample_ts),sqlc.arg(frame_ref),sqlc.arg(width),sqlc.arg(height),sqlc.arg(box),sqlc.arg(score),sqlc.arg(eligible),sqlc.arg(embedding)::text::vector) ON CONFLICT DO NOTHING;

-- name: AdvanceVisualIndex :exec
UPDATE visual_index_sets SET next_sample=sqlc.arg(next_sample),status=sqlc.arg(status),error=sqlc.arg(error),active=CASE WHEN sqlc.arg(status)::text='succeeded' THEN true ELSE active END,updated_at=now() WHERE id=sqlc.arg(id);

-- name: AdvanceFaceIndex :exec
UPDATE face_index_sets SET next_sample=sqlc.arg(next_sample),status=sqlc.arg(status),error=sqlc.arg(error),active=CASE WHEN sqlc.arg(status)::text='succeeded' THEN true ELSE active END,updated_at=now() WHERE id=sqlc.arg(id);

-- name: RetireVisualIndexes :exec
UPDATE visual_index_sets old SET active=false FROM visual_index_sets current WHERE current.id=sqlc.arg(id) AND old.video_id=current.video_id AND old.id<>current.id AND old.interval_seconds=current.interval_seconds AND old.start_ts=current.start_ts AND old.end_ts=current.end_ts;

-- name: RetireFaceIndexes :exec
UPDATE face_index_sets old SET active=false FROM face_index_sets current WHERE current.id=sqlc.arg(id) AND old.video_id=current.video_id AND old.id<>current.id;

-- name: SearchVisualEmbeddings :many
SELECT v.id AS video_id,v.title,v.uploader,e.sample_ts,e.frame_ref,(1-(e.embedding <=> sqlc.arg(embedding)::text::vector))::float8 AS similarity,s.next_sample,s.interval_seconds,s.start_ts,s.end_ts,s.status
FROM visual_frame_embeddings e JOIN visual_index_sets s ON s.id=e.set_id JOIN videos v ON v.id=s.video_id LEFT JOIN channels c ON c.id=v.channel_row_id
WHERE s.model_id=sqlc.arg(model_id) AND (s.active OR NOT EXISTS(SELECT 1 FROM visual_index_sets a WHERE a.video_id=s.video_id AND a.active))
AND (sqlc.narg(video_id)::uuid IS NULL OR v.id=sqlc.narg(video_id))
AND (sqlc.narg(channel_id)::uuid IS NULL OR v.channel_row_id=sqlc.narg(channel_id))
AND (sqlc.narg(creator_id)::uuid IS NULL OR c.creator_id=sqlc.narg(creator_id))
ORDER BY e.embedding <=> sqlc.arg(embedding)::text::vector LIMIT sqlc.arg(candidate_limit);

-- name: CreateVisualReference :one
INSERT INTO visual_references(owner_id,model_id,embedding) VALUES(sqlc.arg(owner_id),sqlc.arg(model_id),sqlc.arg(embedding)::text::vector) RETURNING id,expires_at;

-- name: GetVisualReference :one
SELECT model_id,embedding::text AS embedding FROM visual_references WHERE id=sqlc.arg(id) AND owner_id=sqlc.arg(owner_id) AND expires_at>now();

-- name: ListVideoFaces :many
SELECT f.id,f.sample_ts,f.frame_ref,f.width,f.height,f.box,f.score,f.eligible,f.person_id,f.assignment,p.name,s.video_id
FROM face_observations f JOIN face_index_sets s ON s.id=f.set_id LEFT JOIN people p ON p.id=f.person_id
WHERE NOT f.dismissed AND (s.active OR NOT EXISTS(SELECT 1 FROM face_index_sets a WHERE a.video_id=s.video_id AND a.active))
AND (sqlc.narg(video_id)::uuid IS NULL OR s.video_id=sqlc.narg(video_id))
AND (sqlc.narg(person_id)::uuid IS NULL OR f.person_id=sqlc.narg(person_id))
AND (sqlc.narg(channel_id)::uuid IS NULL OR EXISTS(SELECT 1 FROM videos v WHERE v.id=s.video_id AND v.channel_row_id=sqlc.narg(channel_id)))
AND (sqlc.narg(start_ts)::float8 IS NULL OR f.sample_ts>=sqlc.narg(start_ts))
AND (sqlc.narg(end_ts)::float8 IS NULL OR f.sample_ts<=sqlc.narg(end_ts))
ORDER BY s.video_id,f.sample_ts,f.id LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: QueueVisualRange :one
INSERT INTO ml_jobs(video_id,kind,priority,transcript_hash,model_digest,prompt_version,checkpoint)
VALUES(sqlc.arg(video_id),'visual_index',300,sqlc.arg(asset_fingerprint),sqlc.arg(model_id),sqlc.arg(recipe),sqlc.arg(checkpoint))
ON CONFLICT(video_id,kind,transcript_hash,model_digest,prompt_version) DO UPDATE SET retry_at=now(),status=CASE WHEN ml_jobs.status IN ('failed','waiting_assets','waiting_model') THEN 'queued' ELSE ml_jobs.status END
RETURNING id;

-- name: SearchPeople :many
SELECT p.*,f.frame_ref,s.video_id,f.sample_ts FROM people p LEFT JOIN face_observations f ON f.id=p.representative_id LEFT JOIN face_index_sets s ON s.id=f.set_id
WHERE p.merged_into IS NULL AND (NOT p.hidden OR sqlc.arg(include_hidden)::boolean) AND p.name ILIKE '%'||sqlc.arg(query)::text||'%' ORDER BY p.name,p.id LIMIT 100;

-- name: CreatePerson :one
INSERT INTO people(name,representative_id) VALUES(sqlc.arg(name),sqlc.narg(representative_id)) RETURNING *;

-- name: UpdatePerson :one
UPDATE people SET name=COALESCE(sqlc.narg(name),name),hidden=COALESCE(sqlc.narg(hidden),hidden),creator_id=COALESCE(sqlc.narg(creator_id),creator_id),
representative_id=COALESCE(sqlc.narg(representative_id),representative_id),revision=revision+1 WHERE id=sqlc.arg(id) AND revision=sqlc.arg(revision) AND merged_into IS NULL RETURNING *;

-- name: ListUngroupedFaces :many
SELECT f.id,f.embedding::text AS embedding,f.sample_ts,s.video_id FROM face_observations f JOIN face_index_sets s ON s.id=f.set_id
WHERE s.model_id=sqlc.arg(model_id) AND s.active AND f.eligible AND NOT f.dismissed AND f.person_id IS NULL AND f.assignment='unassigned' ORDER BY f.grouping_checked_at NULLS FIRST,f.id LIMIT 1000;

-- name: FacePersonCandidates :many
SELECT p.id AS person_id,(f.embedding <=> sqlc.arg(embedding)::text::vector)::float8 AS distance FROM people p
JOIN face_observations f ON f.id=p.representative_id JOIN face_index_sets s ON s.id=f.set_id
WHERE s.model_id=sqlc.arg(model_id) AND NOT f.dismissed AND f.eligible AND p.merged_into IS NULL
AND f.embedding <=> sqlc.arg(embedding)::text::vector <= 0.4 ORDER BY distance LIMIT 2;

-- name: FaceSupportCount :one
SELECT count(DISTINCT (s.video_id,floor(f.sample_ts/30)))::int FROM face_observations f JOIN face_index_sets s ON s.id=f.set_id
WHERE s.model_id=sqlc.arg(model_id) AND s.active AND f.eligible AND NOT f.dismissed AND f.embedding <=> sqlc.arg(embedding)::text::vector <= 0.4;

-- name: AssignAutomaticFace :exec
UPDATE face_observations SET person_id=sqlc.arg(person_id),assignment='automatic' WHERE id=sqlc.arg(id) AND assignment='unassigned' AND NOT dismissed;

-- name: GetFaceObservation :one
SELECT f.id,f.set_id,f.sample_ts,f.box,f.person_id,s.video_id,s.asset_fingerprint FROM face_observations f JOIN face_index_sets s ON s.id=f.set_id WHERE f.id=sqlc.arg(id);

-- name: CorrectFace :exec
UPDATE face_observations SET person_id=sqlc.narg(person_id),dismissed=sqlc.arg(dismissed),assignment='manual' WHERE id=sqlc.arg(id);

-- name: SaveFaceCorrection :exec
INSERT INTO face_corrections(video_id,asset_fingerprint,sample_ts,box,person_id,dismissed) VALUES(sqlc.arg(video_id),sqlc.arg(asset_fingerprint),sqlc.arg(sample_ts),sqlc.arg(box),sqlc.narg(person_id),sqlc.arg(dismissed))
ON CONFLICT(video_id,asset_fingerprint,sample_ts,box) DO UPDATE SET person_id=EXCLUDED.person_id,dismissed=EXCLUDED.dismissed,updated_at=now();

-- name: RestoreFaceCorrections :exec
WITH matches AS (
SELECT f.id,c.person_id,c.dismissed,row_number() OVER(PARTITION BY f.id ORDER BY face_box_iou(c.box,f.box) DESC,c.updated_at DESC) AS n
FROM face_observations f JOIN face_index_sets s ON s.id=f.set_id JOIN face_corrections c ON c.video_id=s.video_id AND c.asset_fingerprint=s.asset_fingerprint
WHERE s.id=sqlc.arg(set_id) AND abs(c.sample_ts-f.sample_ts)<0.2 AND face_box_iou(c.box,f.box)>0.5
)
UPDATE face_observations f SET person_id=m.person_id,dismissed=m.dismissed,assignment='manual' FROM matches m WHERE f.id=m.id AND m.n=1;

-- name: MarkFaceGroupingChecked :exec
UPDATE face_observations SET grouping_checked_at=now() WHERE id=sqlc.arg(id);

-- name: MergePersonObservations :exec
UPDATE face_observations SET person_id=sqlc.arg(target_id),assignment='manual' WHERE person_id=sqlc.arg(source_id);

-- name: MergePersonCorrections :exec
UPDATE face_corrections SET person_id=sqlc.arg(target_id) WHERE person_id=sqlc.arg(source_id);

-- name: MarkPersonMerged :exec
UPDATE people SET merged_into=sqlc.arg(target_id),revision=revision+1 WHERE id=sqlc.arg(source_id) AND merged_into IS NULL;

-- name: ListVisualProgress :many
SELECT video_id,'visual'::text AS kind,status,next_sample,interval_seconds,start_ts,end_ts,error,updated_at FROM visual_index_sets
 ORDER BY updated_at DESC LIMIT 100;

-- name: NotifyVisualChange :exec
SELECT pg_notify('visual_changed','');

-- name: GetFaceDimensions :one
SELECT width,height FROM face_observations WHERE id=sqlc.arg(id);

-- name: ListVisionVideos :many
SELECT id,title AS label FROM videos WHERE media='file' AND (title ILIKE '%'||sqlc.arg(query)::text||'%' OR uploader ILIKE '%'||sqlc.arg(query)::text||'%') ORDER BY created_at DESC LIMIT 200;

-- name: ListVisionChannels :many
SELECT id,coalesce(nullif(uploader,''),canonical_url) AS label FROM channels ORDER BY uploader LIMIT 1000;

-- name: ListVisionCreators :many
SELECT id,name AS label FROM creators ORDER BY name LIMIT 1000;

-- name: SetPersonRepresentative :execrows
UPDATE people p SET representative_id=sqlc.arg(face_id),revision=revision+1
WHERE p.id=sqlc.arg(person_id) AND p.merged_into IS NULL AND EXISTS(SELECT 1 FROM face_observations f WHERE f.id=sqlc.arg(face_id) AND f.person_id=p.id AND NOT f.dismissed);

-- name: GetActivePerson :one
SELECT * FROM people WHERE id=sqlc.arg(id) AND merged_into IS NULL FOR UPDATE;

-- name: SaveMergedFaceCorrections :exec
INSERT INTO face_corrections(video_id,asset_fingerprint,sample_ts,box,person_id,dismissed)
SELECT s.video_id,s.asset_fingerprint,f.sample_ts,f.box,sqlc.arg(target_id),f.dismissed FROM face_observations f JOIN face_index_sets s ON s.id=f.set_id WHERE f.person_id=sqlc.arg(source_id)
ON CONFLICT(video_id,asset_fingerprint,sample_ts,box) DO UPDATE SET person_id=EXCLUDED.person_id,updated_at=now();
