-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION protect_published_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status='published' THEN
  RAISE EXCEPTION 'published matrix version is immutable' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
-- +goose StatementEnd
CREATE TRIGGER version_immutable BEFORE UPDATE OR DELETE ON matrix_versions FOR EACH ROW EXECUTE FUNCTION protect_published_version();
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION protect_published_content() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE old_version uuid; new_version uuid; version_status text;
BEGIN
 IF TG_OP<>'INSERT' THEN old_version=OLD.matrix_version_id; END IF;
 IF TG_OP<>'DELETE' THEN new_version=NEW.matrix_version_id; END IF;
 -- Lock both ends of a move in deterministic order. Publication waits for
 -- content changes and content changes wait for in-flight publication.
 FOR version_status IN SELECT status FROM matrix_versions WHERE id IN (old_version,new_version) ORDER BY id FOR SHARE LOOP
  IF version_status='published' THEN
   RAISE EXCEPTION 'published matrix is immutable' USING ERRCODE='23514';
  END IF;
 END LOOP;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER version_immutable ON matrix_versions;
DROP FUNCTION protect_published_version();
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION protect_published_content() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v uuid;
BEGIN
 IF TG_OP='DELETE' THEN v=OLD.matrix_version_id; ELSE v=NEW.matrix_version_id; END IF;
 IF EXISTS(SELECT 1 FROM matrix_versions WHERE id=v AND status='published') THEN RAISE EXCEPTION 'published matrix is immutable' USING ERRCODE='23514'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
-- +goose StatementEnd
