-- +goose Up
-- +goose StatementBegin

-- rate_events is the one operational table spec.md explicitly permits
-- outside the four-table domain model. It is pruned nightly to the last
-- 24 hours plus a small safety margin, so row counts stay bounded.
--
-- Shape: every time a device creates a tree or an observation, we append
-- one row. The limiter counts rows in the last rolling 24 hours, scoped
-- by device (for all kinds) or by IP (for tree creates).
CREATE TYPE rate_event_kind AS ENUM ('tree_create', 'observation_create');

CREATE TABLE rate_events (
  id         uuid            PRIMARY KEY,
  kind       rate_event_kind NOT NULL,
  device_id  uuid            NOT NULL,
  ip_addr    inet            NOT NULL,
  created_at timestamptz     NOT NULL DEFAULT now()
);

-- The two hot-read patterns at write-time.
CREATE INDEX rate_events_device_kind_recent
  ON rate_events (device_id, kind, created_at DESC);
CREATE INDEX rate_events_ip_kind_recent
  ON rate_events (ip_addr, kind, created_at DESC);

-- Supports the nightly prune.
CREATE INDEX rate_events_created_at
  ON rate_events (created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS rate_events;
DROP TYPE  IF EXISTS rate_event_kind;
-- +goose StatementEnd
