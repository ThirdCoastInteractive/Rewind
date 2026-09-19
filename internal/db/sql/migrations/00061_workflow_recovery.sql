-- +goose Up
CREATE TABLE vision_asset_checks(video_id uuid NOT NULL REFERENCES videos(id),kind text NOT NULL,checked_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(video_id,kind));
ALTER TABLE face_observations ADD COLUMN grouping_checked_at timestamptz;
-- +goose StatementBegin
CREATE FUNCTION face_box_iou(a jsonb,b jsonb) RETURNS double precision LANGUAGE sql IMMUTABLE AS $$
 WITH boxes AS (SELECT (a->>'x1')::float8 ax1,(a->>'y1')::float8 ay1,(a->>'x2')::float8 ax2,(a->>'y2')::float8 ay2,(b->>'x1')::float8 bx1,(b->>'y1')::float8 by1,(b->>'x2')::float8 bx2,(b->>'y2')::float8 by2),
 areas AS (SELECT greatest(0,least(ax2,bx2)-greatest(ax1,bx1))*greatest(0,least(ay2,by2)-greatest(ay1,by1)) AS overlap,(ax2-ax1)*(ay2-ay1)+(bx2-bx1)*(by2-by1) AS combined FROM boxes)
 SELECT coalesce(overlap/nullif(combined-overlap,0),0) FROM areas
$$;
-- +goose StatementEnd
ALTER TABLE ml_jobs DROP CONSTRAINT ml_jobs_status_check;
ALTER TABLE ml_jobs ADD CONSTRAINT ml_jobs_status_check CHECK(status IN ('queued','processing','paused','waiting_model','waiting_assets','retry_wait','succeeded','failed'));
-- +goose StatementBegin
CREATE FUNCTION context_window_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_notify('context_windows_changed',COALESCE(NEW.video_id,OLD.video_id)::text);
 RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER context_windows_live AFTER INSERT OR UPDATE OR DELETE ON context_windows FOR EACH ROW EXECUTE FUNCTION context_window_changed();

-- +goose Down
DROP TRIGGER context_windows_live ON context_windows;
DROP FUNCTION context_window_changed();
DROP TABLE vision_asset_checks;
DROP FUNCTION face_box_iou(jsonb,jsonb);
ALTER TABLE face_observations DROP COLUMN grouping_checked_at;
