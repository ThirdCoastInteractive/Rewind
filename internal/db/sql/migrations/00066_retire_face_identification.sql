-- +goose Up
-- Retire the removed pipeline without deleting observations, media, or history.
UPDATE ml_jobs SET status='superseded', last_error='Face identification has been removed',
 lease_token=NULL, locked_at=NULL, locked_by='', updated_at=now()
 WHERE kind='face_index' AND status NOT IN ('succeeded','failed','superseded');

-- Reject new work from an older service during a rolling deployment.
-- +goose StatementBegin
CREATE FUNCTION reject_retired_face_jobs() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='face_index' AND NEW.status NOT IN ('succeeded','failed','superseded') THEN
  RAISE EXCEPTION 'Face identification has been removed';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER reject_retired_face_jobs BEFORE INSERT OR UPDATE OF status ON ml_jobs
 FOR EACH ROW EXECUTE FUNCTION reject_retired_face_jobs();

-- +goose Down
DROP TRIGGER IF EXISTS reject_retired_face_jobs ON ml_jobs;
DROP FUNCTION IF EXISTS reject_retired_face_jobs();
-- Previously retired work stays retired; historical observations remain intact.
