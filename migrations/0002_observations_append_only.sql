-- +goose Up
-- +goose StatementBegin

-- observations are append-only. The archive invariant is load-bearing: it
-- is what lets the dataset, decades from now, be cited by researchers or
-- used as civic memory. Sealing the invariant at the database layer means
-- a careless application-level change in 2028 cannot silently erode it.
--
-- The only field that may change is hidden_at, and only from NULL to a
-- timestamp (set once, never cleared).

CREATE OR REPLACE FUNCTION observations_reject_archive_mutation() RETURNS trigger AS $$
BEGIN
  IF NEW.id                 IS DISTINCT FROM OLD.id
  OR NEW.tree_id            IS DISTINCT FROM OLD.tree_id
  OR NEW.bloom_state        IS DISTINCT FROM OLD.bloom_state
  OR NEW.photo_r2_key       IS DISTINCT FROM OLD.photo_r2_key
  OR NEW.observed_at        IS DISTINCT FROM OLD.observed_at
  OR NEW.reported_by_device IS DISTINCT FROM OLD.reported_by_device THEN
    RAISE EXCEPTION 'observations are append-only: only hidden_at may change';
  END IF;
  IF OLD.hidden_at IS NOT NULL AND NEW.hidden_at IS DISTINCT FROM OLD.hidden_at THEN
    RAISE EXCEPTION 'hidden_at is set-once: cannot be cleared or overwritten';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER observations_no_archive_mutation
BEFORE UPDATE ON observations
FOR EACH ROW EXECUTE FUNCTION observations_reject_archive_mutation();

-- DELETE is also forbidden. Once observed, an observation stays in the
-- archive. Hiding is the only way to remove something from public view.
CREATE OR REPLACE FUNCTION observations_reject_delete() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'observations are append-only: DELETE is forbidden, use hide';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER observations_no_delete
BEFORE DELETE ON observations
FOR EACH ROW EXECUTE FUNCTION observations_reject_delete();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS observations_no_archive_mutation ON observations;
DROP TRIGGER IF EXISTS observations_no_delete ON observations;
DROP FUNCTION IF EXISTS observations_reject_archive_mutation();
DROP FUNCTION IF EXISTS observations_reject_delete();
-- +goose StatementEnd
