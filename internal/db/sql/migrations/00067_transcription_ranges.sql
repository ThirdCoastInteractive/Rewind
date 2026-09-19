-- +goose Up
ALTER TABLE video_transcripts ADD COLUMN coverage jsonb;
COMMENT ON COLUMN video_transcripts.coverage IS 'NULL means full-source transcript; otherwise array of transcribed [start,end] seconds, not complete video coverage';
ALTER TABLE ml_jobs ADD COLUMN range_start double precision;
ALTER TABLE ml_jobs ADD COLUMN range_end double precision;
ALTER TABLE ml_jobs ADD CONSTRAINT ml_transcription_range CHECK (
 (range_start IS NULL AND range_end IS NULL) OR
 (kind='transcribe' AND range_start IS NOT NULL AND range_end IS NOT NULL AND range_start >= 0 AND range_end > range_start AND range_end < 'Infinity'::double precision)
);

-- +goose StatementBegin
CREATE FUNCTION repair_duration_from_probe() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d text; seconds numeric;
BEGIN
 IF NEW.duration_seconds IS NULL OR NEW.duration_seconds<=0 THEN
  d:=NEW.probe_data->'format'->>'duration';
  IF length(d)<32 AND d ~ '^[0-9]+([.][0-9]+)?$' THEN
   seconds:=ceil(d::numeric);
   IF seconds>0 AND seconds<2147483647 THEN NEW.duration_seconds:=seconds::integer; END IF;
  END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER videos_duration_from_probe BEFORE INSERT OR UPDATE OF duration_seconds,probe_data ON videos FOR EACH ROW EXECUTE FUNCTION repair_duration_from_probe();
UPDATE videos SET probe_data=probe_data WHERE (duration_seconds IS NULL OR duration_seconds<=0) AND probe_data IS NOT NULL;

-- +goose Down
DROP TRIGGER videos_duration_from_probe ON videos;
DROP FUNCTION repair_duration_from_probe();
ALTER TABLE ml_jobs DROP CONSTRAINT ml_transcription_range;
ALTER TABLE ml_jobs DROP COLUMN range_start, DROP COLUMN range_end;
ALTER TABLE video_transcripts DROP COLUMN coverage;
