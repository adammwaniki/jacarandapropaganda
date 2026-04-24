-- +goose Up
-- +goose StatementBegin

-- Extensions. Idempotent so a fresh database or one initialized by
-- docker-entrypoint-initdb.d both arrive at the same state.
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS postgis_raster;  -- dependency of h3_postgis
CREATE EXTENSION IF NOT EXISTS h3;
CREATE EXTENSION IF NOT EXISTS h3_postgis;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- bloom_state enum. Values are from spec.md (newer, 4-state) and are the
-- source of truth. Adding or renaming values requires a new migration
-- because Postgres enum membership is immutable by design.
CREATE TYPE bloom_state AS ENUM ('budding', 'partial', 'full', 'fading');

CREATE TYPE moderation_target_kind AS ENUM ('tree', 'observation');
CREATE TYPE moderation_resolution  AS ENUM ('hidden', 'dismissed');

-- -------------------------------------------------------------------
-- devices
--   UUIDv4 by design: the id lives in a user cookie, and a time-ordered
--   id would leak the first-visit timestamp to anyone inspecting it.
-- -------------------------------------------------------------------
CREATE TABLE devices (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  first_seen  timestamptz NOT NULL DEFAULT now(),
  last_seen   timestamptz NOT NULL DEFAULT now(),
  blocked_at  timestamptz
);

-- -------------------------------------------------------------------
-- trees
--   UUIDv7 for append-heavy index locality. Generated in Go; we do not
--   rely on a server-side v7 function so the generator stays in one place.
--   location is geography(Point, 4326) — correct spherical distance math
--   for ST_DWithin(... , 3) to mean "3 meters".
-- -------------------------------------------------------------------
CREATE TABLE trees (
  id                 uuid                 PRIMARY KEY,
  location           geography(Point, 4326) NOT NULL,
  h3_cell_r9         bigint               NOT NULL,
  h3_cell_r7         bigint               NOT NULL,
  species            text                 NOT NULL DEFAULT 'jacaranda',
  created_at         timestamptz          NOT NULL DEFAULT now(),
  created_by_device  uuid                 NOT NULL REFERENCES devices(id),
  hidden_at          timestamptz
);

CREATE INDEX trees_location_gix      ON trees USING GIST (location);
CREATE INDEX trees_h3_r9_idx         ON trees (h3_cell_r9) WHERE hidden_at IS NULL;
CREATE INDEX trees_h3_r7_idx         ON trees (h3_cell_r7) WHERE hidden_at IS NULL;
CREATE INDEX trees_species_visible   ON trees (species)    WHERE hidden_at IS NULL;
CREATE INDEX trees_created_by_device ON trees (created_by_device);

-- -------------------------------------------------------------------
-- observations
--   Append-only. A tree's current state is the most recent non-hidden
--   observation. Historical observations are the archive and must never
--   be mutated — enforced at the repository layer and by tests.
-- -------------------------------------------------------------------
CREATE TABLE observations (
  id                  uuid        PRIMARY KEY,
  tree_id             uuid        NOT NULL REFERENCES trees(id) ON DELETE RESTRICT,
  bloom_state         bloom_state NOT NULL,
  photo_r2_key        text,
  observed_at         timestamptz NOT NULL DEFAULT now(),
  reported_by_device  uuid        NOT NULL REFERENCES devices(id),
  hidden_at           timestamptz
);

CREATE INDEX observations_tree_recent
  ON observations (tree_id, observed_at DESC)
  WHERE hidden_at IS NULL;

CREATE INDEX observations_reported_by_device_recent
  ON observations (reported_by_device, observed_at DESC);

-- -------------------------------------------------------------------
-- moderation_queue
--   Small, operator-facing. Three reports from distinct devices auto-hide
--   the target at the application layer; this table remains the log.
-- -------------------------------------------------------------------
CREATE TABLE moderation_queue (
  id               uuid                    PRIMARY KEY,
  target_kind      moderation_target_kind  NOT NULL,
  target_id        uuid                    NOT NULL,
  reason           text,
  reporter_device  uuid                    NOT NULL REFERENCES devices(id),
  created_at       timestamptz             NOT NULL DEFAULT now(),
  resolved_at      timestamptz,
  resolution       moderation_resolution
);

CREATE INDEX moderation_queue_unresolved
  ON moderation_queue (created_at DESC)
  WHERE resolved_at IS NULL;

CREATE INDEX moderation_queue_target
  ON moderation_queue (target_kind, target_id);

-- Distinct-reporter count per (target_kind, target_id) is read often at
-- write time (to fire the auto-hide rule); this unique index both speeds
-- that query and prevents a single device from inflating the count by
-- spamming reports on the same target.
CREATE UNIQUE INDEX moderation_queue_one_report_per_device
  ON moderation_queue (target_kind, target_id, reporter_device);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS moderation_queue;
DROP TABLE IF EXISTS observations;
DROP TABLE IF EXISTS trees;
DROP TABLE IF EXISTS devices;

DROP TYPE IF EXISTS moderation_resolution;
DROP TYPE IF EXISTS moderation_target_kind;
DROP TYPE IF EXISTS bloom_state;

-- Extensions are not dropped on Down. They are shared resources and
-- tearing them down when other databases in the cluster may depend on
-- them would be a footgun. A full teardown should drop the database.
-- +goose StatementEnd
