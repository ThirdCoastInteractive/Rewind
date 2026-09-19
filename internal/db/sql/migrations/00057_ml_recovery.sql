-- +goose Up
ALTER TABLE ml_jobs ADD COLUMN failure_count integer NOT NULL DEFAULT 0,
 ADD COLUMN lease_token uuid, ADD COLUMN retry_at timestamptz NOT NULL DEFAULT now(), ADD COLUMN checkpoint jsonb NOT NULL DEFAULT '{}';
ALTER TABLE ml_jobs DROP CONSTRAINT ml_jobs_status_check;
ALTER TABLE ml_jobs ADD CONSTRAINT ml_jobs_status_check CHECK(status IN ('queued','processing','waiting_model','waiting_assets','retry_wait','succeeded','failed'));
ALTER TABLE ml_jobs DROP CONSTRAINT ml_jobs_kind_check;
ALTER TABLE ml_jobs ADD CONSTRAINT ml_jobs_kind_check CHECK(kind IN ('transcribe','refine_boundaries','context_windows','visual_index','face_index'));
ALTER TABLE ml_jobs DROP CONSTRAINT ml_jobs_video_id_kind_transcript_hash_key;
ALTER TABLE ml_jobs ADD UNIQUE(video_id,kind,transcript_hash,model_digest,prompt_version);
DROP TRIGGER ml_jobs_notify_trigger ON ml_jobs;
CREATE TRIGGER ml_jobs_notify_trigger AFTER INSERT OR UPDATE OF status ON ml_jobs FOR EACH ROW EXECUTE FUNCTION notify_ml_jobs();
CREATE TABLE context_window_chunks (set_id uuid NOT NULL REFERENCES context_window_sets(id), ordinal int NOT NULL, output jsonb NOT NULL, PRIMARY KEY(set_id,ordinal));
-- Retain duplicate historical rows, moving only duplicate ordinals out of the generated range.
WITH ranked AS (SELECT id, row_number() OVER(PARTITION BY set_id,ordinal ORDER BY created_at,id) AS n FROM context_windows WHERE set_id IS NOT NULL),
 duplicates AS (SELECT id, row_number() OVER(ORDER BY id) AS n FROM ranked WHERE n>1)
UPDATE context_windows cw SET ordinal=-duplicates.n::int, stale=true FROM duplicates WHERE cw.id=duplicates.id;
CREATE UNIQUE INDEX context_windows_set_ordinal_unique ON context_windows(set_id,ordinal);

-- The shared transcript write path is enforced in the database so imports and
-- Whisper cannot diverge. Hash precisely the canonical language, text and timing.
-- +goose StatementBegin
CREATE FUNCTION transcript_fingerprint(target uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(lang::text || E'\n' || text || E'\n' || cues::text,'UTF8')),'hex')
 FROM video_transcripts WHERE video_id=target ORDER BY CASE WHEN lang::text='en' THEN 0 ELSE 1 END,lang LIMIT 1
$$;
CREATE FUNCTION enqueue_transcript_context() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.text IS NOT DISTINCT FROM OLD.text AND NEW.cues IS NOT DISTINCT FROM OLD.cues THEN RETURN NEW; END IF;
 INSERT INTO ml_jobs(video_id,kind,priority,transcript_hash,prompt_version)
 VALUES(NEW.video_id,'context_windows',200,transcript_fingerprint(NEW.video_id),'context-v3-bytes-24k') ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER transcript_context_job AFTER INSERT OR UPDATE ON video_transcripts FOR EACH ROW EXECUTE FUNCTION enqueue_transcript_context();
INSERT INTO ml_jobs(video_id,kind,priority,transcript_hash,prompt_version)
SELECT DISTINCT video_id,'context_windows',200,transcript_fingerprint(video_id),'context-v3-bytes-24k' FROM video_transcripts WHERE text<>'' ON CONFLICT DO NOTHING;

-- +goose Down
DROP TRIGGER IF EXISTS transcript_context_job ON video_transcripts;
DROP FUNCTION IF EXISTS enqueue_transcript_context();
DROP FUNCTION IF EXISTS transcript_fingerprint(uuid);
DROP INDEX IF EXISTS context_windows_set_ordinal_unique;
DROP TABLE IF EXISTS context_window_chunks;
-- Preserve expanded job statuses/keys when rolling application code back. They
-- cannot be narrowed losslessly after new jobs have been created.
