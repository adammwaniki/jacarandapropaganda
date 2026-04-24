# JacarandaPropaganda — Architecture

*Companion to `spec.md` and `docs/build-plan.md`. This document describes the implemented shape of the system as it grows. Amend it when reality diverges from intention.*

## One binary, one database, one bucket

```
┌──────────────┐
│  Cloudflare  │   TLS, CDN, DDoS protection
└──────┬───────┘
       │
   ┌───┴───────────────────┬───────────────┐
   ▼                       ▼               ▼
┌───────────┐        ┌─────────────┐  ┌────────────┐
│ Go binary │  ◄──► │ PostgreSQL  │  │ Cloudflare │
│ (systemd) │        │  + PostGIS  │  │   R2       │
│           │        │  + h3-pg    │  │  photos,   │
│           │        └─────────────┘  │  PMTiles   │
└───────────┘                         └────────────┘
```

One VPS, one Postgres, one R2 bucket. No Redis, no Kubernetes, no ORM.
See `spec.md` for the reasoning behind each absence.

## Package layout

| Package | Responsibility |
|---|---|
| `cmd/server` | Entry point — parses env, builds router, runs `http.Server`. |
| `internal/app` | HTTP handlers, middleware, HTML templates. The only package that touches `http.ResponseWriter`. |
| `internal/geo` | Pure spatial helpers — bbox parsing, GeoJSON encoding, H3 cell math. No DB. |
| `internal/store` | pgx-backed repositories (trees, observations, devices, moderation) + goose migration runner. |
| `internal/store/testutil` | Creates throwaway databases for integration tests. |
| `migrations` | Goose SQL migrations, embedded into the binary. |
| `web` | HTML templates + static assets, embedded via `//go:embed`. |
| `docker/postgres` | Dockerfile for our custom Postgres 16 + PostGIS 3.4 + h3-pg image. |

The `internal/` boundary is load-bearing: nothing outside this binary imports our code.

## Test strategy

See `docs/build-plan.md` § "Testing layers and tooling". Summary:

- **Unit tests** run on every commit with the default build tag.
- **Integration tests** (`-tags=integration`) require the docker-compose stack to be running locally, or the equivalent service container in CI. They talk to a real Postgres with PostGIS + h3-pg, not to mocks.
- **End-to-end tests** (Playwright, arriving in Phase D) run the full binary against a seeded test DB and MinIO stand-in.
- **No mocks for the database.** PostGIS behavior and h3-pg cell math cannot be meaningfully mocked; a test against a mock of either would encode bugs rather than catch them.

## Migrations

Migrations are embedded into the binary via `migrations/migrations.go` but are **never applied on startup**. The deploy flow is:

1. `scp` the new binary to the VPS.
2. Run `goose up` manually and read the migration output.
3. `systemctl restart jacaranda`.

This is a deliberate tax: it prevents a bad migration from taking down the service during a routine deploy, and it forces the operator to read the migration before applying it.

## Running locally

```
make dev-up            # Postgres (host port 55432) + MinIO (9000/9001)
make test              # unit tests
make test-integration  # integration tests (requires dev-up)
make run               # run the server (set JP_ADDR if 8080 is taken)
make dev-down
```

`JP_ADDR` defaults to `:8080`. Override if another service owns that port.

## What's implemented (Phase A)

- `GET /` — HTML shell with MapLibre + PMTiles + Alpine.js, centered on Nairobi.
- `GET /health` — liveness probe returning `{"status":"ok"}`.
- `GET /trees?bbox=…` — empty GeoJSON FeatureCollection (repository wiring lands in Phase C).
- Goose migrations for the four-table schema (`trees`, `observations`, `devices`, `moderation_queue`) with PostGIS geography columns and H3 r9/r7 columns.
- CI running unit tests + integration tests against a freshly built Postgres image.

## What is deliberately not implemented yet

Anything beyond Phase A in `docs/build-plan.md`. Repository methods, rate limiting, moderation, media handling, visual style, Deck.gl heatmap — all downstream.
