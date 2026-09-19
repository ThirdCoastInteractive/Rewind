-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE embedding_models (
 id text PRIMARY KEY, name text NOT NULL, revision text NOT NULL, recipe text NOT NULL,
 dimensions int NOT NULL CHECK(dimensions=512), license text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE visual_index_sets (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), video_id uuid NOT NULL REFERENCES videos(id), model_id text NOT NULL REFERENCES embedding_models(id),
 asset_fingerprint text NOT NULL, interval_seconds double precision NOT NULL DEFAULT 5, start_ts double precision NOT NULL DEFAULT 0, end_ts double precision NOT NULL,
 status text NOT NULL DEFAULT 'processing', active boolean NOT NULL DEFAULT false, next_sample int NOT NULL DEFAULT 0,
 error text NOT NULL DEFAULT '', updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(video_id,model_id,asset_fingerprint,interval_seconds,start_ts,end_ts)
);
CREATE TABLE face_index_sets (LIKE visual_index_sets INCLUDING ALL);
ALTER TABLE face_index_sets ADD FOREIGN KEY(video_id) REFERENCES videos(id), ADD FOREIGN KEY(model_id) REFERENCES embedding_models(id);
CREATE TABLE visual_frame_embeddings (
 set_id uuid NOT NULL REFERENCES visual_index_sets(id), sample_index int NOT NULL, sample_ts double precision NOT NULL, frame_ref text NOT NULL,
 embedding vector(512) NOT NULL, PRIMARY KEY(set_id,sample_index)
);
CREATE INDEX visual_vectors_cosine ON visual_frame_embeddings USING hnsw(embedding vector_cosine_ops);
CREATE TABLE people (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL DEFAULT '', creator_id uuid REFERENCES creators(id), representative_id uuid,
 hidden boolean NOT NULL DEFAULT false, revision int NOT NULL DEFAULT 1, merged_into uuid REFERENCES people(id), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE face_observations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), set_id uuid NOT NULL REFERENCES face_index_sets(id), sample_index int NOT NULL, face_index int NOT NULL,
 sample_ts double precision NOT NULL, frame_ref text NOT NULL, width int NOT NULL, height int NOT NULL, box jsonb NOT NULL, score double precision NOT NULL,
 eligible boolean NOT NULL, embedding vector(512) NOT NULL, person_id uuid REFERENCES people(id), assignment text NOT NULL DEFAULT 'unassigned',
 dismissed boolean NOT NULL DEFAULT false, UNIQUE(set_id,sample_index,face_index)
);
CREATE INDEX face_vectors_cosine ON face_observations USING hnsw(embedding vector_cosine_ops);
ALTER TABLE people ADD FOREIGN KEY(representative_id) REFERENCES face_observations(id);
CREATE TABLE face_corrections (
 video_id uuid NOT NULL REFERENCES videos(id), asset_fingerprint text NOT NULL, sample_ts double precision NOT NULL, box jsonb NOT NULL,
 person_id uuid REFERENCES people(id), dismissed boolean NOT NULL DEFAULT false, updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(video_id,asset_fingerprint,sample_ts,box)
);
CREATE TABLE face_index_selections (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), video_id uuid REFERENCES videos(id), channel_id uuid REFERENCES channels(id), enabled boolean NOT NULL DEFAULT true,
 CHECK((video_id IS NULL)<>(channel_id IS NULL)), UNIQUE(video_id), UNIQUE(channel_id)
);
CREATE TABLE visual_references (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), owner_id uuid NOT NULL REFERENCES users(id), model_id text NOT NULL REFERENCES embedding_models(id),
 embedding vector(512) NOT NULL, expires_at timestamptz NOT NULL DEFAULT now()+interval '24 hours'
);
-- +goose StatementBegin
CREATE FUNCTION notify_visual_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_notify('visual_changed',''); RETURN NEW; END $$;
-- +goose StatementEnd
CREATE TRIGGER visual_progress AFTER INSERT OR UPDATE ON visual_index_sets FOR EACH STATEMENT EXECUTE FUNCTION notify_visual_change();
CREATE TRIGGER face_progress AFTER INSERT OR UPDATE ON face_index_sets FOR EACH STATEMENT EXECUTE FUNCTION notify_visual_change();
CREATE TRIGGER people_changed AFTER INSERT OR UPDATE ON people FOR EACH STATEMENT EXECUTE FUNCTION notify_visual_change();

-- +goose Down
DROP TABLE visual_references,face_index_selections,face_corrections;
ALTER TABLE people DROP CONSTRAINT people_representative_id_fkey;
DROP TABLE face_observations,people,visual_frame_embeddings,face_index_sets,visual_index_sets,embedding_models;
DROP FUNCTION notify_visual_change();
-- Keep vector installed: other deployments may use it independently.
