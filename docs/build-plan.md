# JacarandaPropaganda — Phased TDD Build Plan

*Companion to `spec.md`. The spec describes what becomes true at each product phase; this plan describes the engineering rhythm that gets us there, written as test-driven development from the first commit.*

---

## Guiding principles

1. **Red → Green → Refactor, per change.** No production code is written without a failing test that motivates it. Refactor only against a green suite.
2. **Test the spec's invariants, not the implementation.** Examples: "a pin within 3m of an existing tree returns the comparison sheet"; "an observation is never mutated, only appended"; "a device cookie is UUIDv4 (random), not UUIDv7 (time-ordered)".
3. **Real Postgres, no mocks for the database layer.** PostGIS spatial behavior and h3-pg cell math cannot be meaningfully mocked. Integration tests run against a real `postgres:16 + postgis + h3-pg` container; a bad schema change must fail locally the same way it would fail in production.
4. **Outside-in for user journeys, inside-out for primitives.** User-facing behavior (pin a tree, update bloom state, see dedup sheet) is specified first by an end-to-end test that fails until the slice is complete. Pure primitives (ID generation, bbox parsing, H3 cell math) are grown inside-out with unit tests.
5. **Test the absences, too.** The spec's restraint is load-bearing. Add tests that assert, for example, that no `users` table exists after migration, that `/metrics` exposes only the four named counters, that `GET /trees` never returns hidden rows.
6. **Prefer deletions over additions in refactor.** Every test kept is a cost. When a higher-level test supersedes a lower-level one, delete the lower-level one.

## Testing layers and tooling

| Layer | Tool | When it runs |
|---|---|---|
| Go unit | `go test ./...` | Every commit (local + CI) |
| Go integration (real Postgres) | `go test -tags=integration` + testcontainers-go (or `docker-compose.test.yml`) | Every commit (local + CI) |
| HTTP handler | `net/http/httptest` against a freshly migrated test DB per test | Every commit |
| Presigned-URL / R2 | MinIO as local S3-compatible stand-in | Every commit |
| Browser E2E | Playwright against the Go binary + test DB + MinIO | Every commit on main paths; nightly for full suite |
| Accessibility | `axe-core` via Playwright; manual NVDA/VoiceOver pass at phase 2 exit | Every commit for axe; manual once per phase |
| Performance | Lighthouse CI (mobile profile, throttled) | PR gate from Phase J onwards |
| Load / soak | `k6` scripting 50 rps mixed read/write for 1 hour | Pre-launch (Phase I) and pre-season each year |

## Repository layout (grown incrementally — do not scaffold ahead of need)

```
/cmd/server           — main.go (the single binary)
/internal/app         — HTTP handlers, middleware, templates
/internal/store       — pgx-backed repositories (trees, observations, devices, moderation)
/internal/geo         — bbox parsing, H3 helpers, GeoJSON encoding
/internal/media       — R2 presign + HEAD verification
/internal/rate        — rate limiter (Postgres-backed)
/migrations           — goose migrations, numbered and forward-only in prod
/web                  — html/template files, Alpine controllers, CSS, map style JSON
/web/e2e              — Playwright tests
/testdata             — fixture trees, observations, golden GeoJSON
/scripts              — dev-up.sh, dev-down.sh, seed.sh, restore-drill.sh
/docs
```

The `internal/` boundary is intentional: nothing outside this binary imports our code. Reject the urge to publish packages.

---

## Phase A — Foundation and the walking skeleton

**Maps to spec Phase 0.**
**Done when:** CI is green on a trivial end-to-end flow — a browser hits a Go binary, the binary reads from a migrated Postgres with PostGIS and h3-pg, and returns an empty GeoJSON `FeatureCollection`. The holding page renders a MapLibre map centered on Nairobi from a PMTiles file on R2.

### Failing tests to write first, in order

1. **`go test ./cmd/server -run TestHealth`** — expects `GET /health` to return `200 {"status":"ok"}`. Fails because the binary does not exist.
2. **`make test-integration`** — spins up Postgres+PostGIS+h3-pg via testcontainers, runs `goose up`, asserts extensions are installed and the four tables exist with expected columns and enum types. Fails because no migrations exist.
3. **Migration round-trip test** — `goose up` then `goose down` leaves the database empty. Fails until every migration is reversible.
4. **`TestTreesEndpoint_EmptyBbox`** — `GET /trees?bbox=36.6,-1.4,37.0,-1.1` returns an empty GeoJSON FeatureCollection with the correct content-type. Fails because no handler exists.
5. **Playwright `shell.spec.ts`** — page loads, MapLibre canvas mounts, map centered within 500m of Nairobi CBD, PMTiles network request to R2 returns 200. Fails until the HTML shell and map style are wired.

### Exit checklist (Phase 0 progress indicator)

- [ ] A phone on cellular data loads the empty map in under 3 seconds (manual check, recorded in `docs/notebook.md`).
- [ ] `docker compose up` yields a working local stack; `scripts/dev-up.sh` prints one line per service with its URL.
- [ ] CI green. Test count baseline recorded (expect ~10–15).
- [ ] `docs/ARCHITECTURE.md` populated with the overview diagram and the test strategy summary.

---

## Phase B — Device identity

**Bridges spec Phase 0 → Phase 1.**
**Done when:** every request is stamped with a `device_id` cookie (UUIDv4), a row in `devices` exists per cookie, and `last_seen` updates on writes without dominating the write path.

### Failing tests to write first

1. `TestDeviceMiddleware_SetsCookieOnFirstRequest` — no cookie in, Set-Cookie out with SameSite=Lax, Secure (behind TLS), HttpOnly, 10-year expiry.
2. `TestDeviceMiddleware_IsUUIDv4NotV7` — parse the cookie, assert version bits. This test encodes the privacy invariant from the spec; do not let it drift.
3. `TestDeviceStore_UpsertFirstSeenOnce` — two requests from the same device result in one row, `first_seen` unchanged, `last_seen` advanced.
4. `TestDeviceStore_BlockedDeviceFlag` — setting `blocked_at` causes the blocked-device code path to take effect (tested properly in Phase H).

### Exit checklist

- [ ] Cookie rotation test (clearing cookie yields a new device row) documented and passing.
- [ ] Middleware is the *only* place that issues device IDs; grep confirms no other caller of `uuid.NewRandom` for this purpose.

---

## Phase C — Tree and observation repositories (the spatial core)

**Maps to spec Phase 1.**
**Done when:** we can insert trees, insert observations on them, query a viewport, and run the 3-meter dedup check — all exercised against real PostGIS and h3-pg.

### Failing tests to write first

1. `TestTreeStore_InsertGeneratesUUIDv7` — check version bits, check that two inserts a millisecond apart are ordered.
2. `TestTreeStore_PopulatesH3Cells` — inserting at `(-1.2921, 36.8219)` (Nairobi CBD) yields non-zero `h3_cell_r9` and `h3_cell_r7`, both matching `h3-pg`'s `latLngToCell` for those resolutions.
3. `TestTreeStore_DedupWithin3mFindsCandidates` — insert a tree at CBD, query `ST_DWithin` with a point 2.5m away → 1 candidate. Move to 3.5m → 0 candidates. This is the spec's central spatial invariant; test it across latitudes (equator vs. -1.3°) to catch meters-vs-degrees bugs.
4. `TestTreeStore_DedupIgnoresHiddenTrees` — `hidden_at` set → not returned as a dedup candidate.
5. `TestTreeStore_BboxQuery` — seed five trees inside a bbox and five outside; the query returns exactly the five inside, as GeoJSON.
6. `TestObservationStore_CurrentStateIsMostRecent` — three observations on one tree in order bud→partial→full; the "current state" query returns `full`. Hide the most recent → returns `partial`. Hide all → tree returns no current state.
7. `TestObservationStore_NeverMutatesHistory` — attempt to update an observation via any store method fails at compile time (method does not exist) or at runtime (repository returns an error). This is an architectural test; it prevents someone adding an `UpdateObservation` method in 2028.
8. `TestBloomStateEnum_RejectsUnknownValue` — inserting `bloom_state = 'exploding'` fails at the DB layer with a constraint violation, not in Go code. The enum is the source of truth.

### Exit checklist

- [ ] Dedup test passes at four Nairobi coordinates (CBD, Karura, Westlands, Karen) so we don't encode a single-point false positive.
- [ ] `EXPLAIN (ANALYZE)` of the bbox query uses the GIST index on `trees.location` — captured in a test that asserts the plan shape, not runtime.
- [ ] Zero mocks in `internal/store` tests.

---

## Phase D — HTTP surface: create, read, update

**Maps to spec Phase 1.**
**Done when:** the full user journey — open map → tap + → submit pin → see pin — works end-to-end, including the dedup comparison sheet as an HTML fragment swap.

### Failing tests to write first (outside-in)

1. **Playwright `pin-new-tree.spec.ts`** — grant geolocation + camera; tap +; select a bloom state; submit; the new pin appears on the map within 2 seconds. This is the spec's 20-second time-to-pin goal; instrument it.
2. **Playwright `dedup-comparison.spec.ts`** — seed one tree at a known location; script sets geolocation to 2m away; submit; comparison sheet renders the seeded tree's photo, bloom state, and distance. Pick "This is the same tree" → an observation is written on the existing tree, no new tree row.
3. **Playwright `update-existing-pin.spec.ts`** — tap a seeded pin; detail sheet opens; tap a new bloom state; the pin's color/state updates on the map.
4. **`TestHandler_PostTrees_CreatesTreeAndObservationAtomically`** — integration test: a transaction failure on the observation insert rolls back the tree. No orphan trees.
5. **`TestHandler_PostTrees_ReturnsComparisonFragmentWithin3m`** — asserts the response is an HTML fragment (for Alpine AJAX swap), not JSON, with the candidate trees' photos as `<img>` tags.
6. **`TestHandler_GetTrees_RejectsOversizedBbox`** — a bbox spanning > ~100km² returns 400. Prevents accidental whole-world queries.
7. **`TestHandler_GetTrees_CachesCorrectlyInEdge`** — response has `Cache-Control: public, max-age=30` (or similar); ETag present. This matters because Cloudflare fronts us.

### Exit checklist

- [ ] Time-to-pin from cold cache measured on a throttled 3G Playwright run: < 20 seconds (recorded).
- [ ] Every handler test runs in < 100ms individually; full HTTP suite < 10s.
- [ ] No handler returns JSON for a write; only GeoJSON (read) and HTML fragments (write) cross the wire. Tested.

---

## Phase E — Rate limiting and abuse control

**Maps to spec Phase 1.**
**Done when:** the two-signal limit (10/device/24h trees, 30/IP/24h trees, 60/device/24h observations) is enforced in Postgres, with a plain-language 429 response.

### Failing tests to write first

1. `TestRate_DeviceTreeLimit_10th_OK_11th_429` — seed 10 trees for a device in the last 24h; the 11th returns 429 with a human-readable message ("You've pinned 10 trees today. Come back tomorrow.").
2. `TestRate_IPTreeLimit_30th_OK_31st_429` — across multiple devices sharing an IP.
3. `TestRate_ObservationLimit_60th_OK_61st_429` — per device, separate counter.
4. `TestRate_RollingWindowNotCalendarDay` — a tree created 23h 59m ago still counts; a tree created 24h 01m ago does not.
5. `TestRate_PruneJobRemovesExpiredCounters` — the nightly prune leaves the counters table small (tested by row count before/after).

### Exit checklist

- [ ] No Redis. Limiter state lives in a single Postgres table, pruned nightly by a goroutine on the binary with leader election via `pg_try_advisory_lock`.
- [ ] The 429 response body is a friendly HTML fragment, not JSON.

---

## Phase F — Media handling (presigned uploads + HEAD verification)

**Maps to spec Phase 1 / Phase 2.**
**Done when:** the browser gets a presigned URL bounded by content-length and content-type, and a post-upload HEAD confirms compliance.

### Failing tests to write first

1. `TestPresign_ReturnsURLWithContentLengthCeiling` — parse the presigned policy; assert `content-length-range` is `[1, 1_000_000]` and `Content-Type` must be `image/jpeg` or `image/webp`.
2. `TestPresign_KeyScopedToDevicePlusUUIDv7` — the object key matches `photos/<YYYY>/<MM>/<device_prefix>/<uuidv7>.jpg` pattern; prevents cross-device key collisions and makes lifecycle rules trivial later.
3. **Integration against MinIO**: `TestR2_UploadAt500KB_Succeeds`, `TestR2_UploadAt2MB_Fails` — proves the policy is actually enforced, not just present in the URL.
4. `TestMedia_HeadVerification_MismatchFlagsToModeration` — simulate a client that lies about Content-Type in the PUT; after upload, the HEAD verifier detects mismatch and inserts a pre-hidden row into `moderation_queue`.
5. **Playwright `photo-compression.spec.ts`** — upload a 4MB source image; assert the outgoing PUT body is ≤ 500KB (client-side compression happened).

### Exit checklist

- [ ] A hostile `curl` script that tries to PUT a 500MB non-image to R2 is demonstrated to fail; the attempt and its failure are captured in `docs/security-evidence.md`.
- [ ] Photo URLs are served through Cloudflare CDN; no Go handler proxies photo bytes.

---

## Phase G — Frontend polish: visual style, pins, filters

**Maps to spec Phase 2.**
**Done when:** the map looks like something you would sign — muted base layer, custom purple pins per bloom state, stats bar live, client-side filter toggles.

### Failing tests to write first

1. **Visual regression** via Playwright screenshots: `map-default.png`, `pin-budding.png`, `pin-partial.png`, `pin-full.png`, `pin-fading.png` at three zoom levels (city, neighborhood, street). A pixel-diff > 0.1% fails the test; updating the golden image is a deliberate, reviewed action.
2. `TestFilter_BloomStateToggle_NoServerRoundTrip` — Alpine filter off/on toggles pin visibility with zero network requests. Asserted via Playwright's request interception.
3. `TestStatsBar_UpdatesAfterPinCreation` — create a pin via UI; stats bar "trees" count increments without page reload.
4. **Axe-core accessibility sweep** — every Playwright page asserts zero axe violations at `serious` or `critical` severity.
5. **Keyboard-only navigation test** — a user can place a pin using only Tab, Enter, and arrow keys (for map pan); no mouse events in the Playwright trace.
6. **Lighthouse CI**: mobile scores ≥ 95 for performance, accessibility, best-practices on the home page. CI fails the PR otherwise.

### Exit checklist

- [ ] Map style JSON committed under `/web/style/jacaranda.json`; iteration count ≥ 20 recorded in commit history by message prefix `style:`.
- [ ] Soft-launch seed script has planted ≥ 200 real pins in staging; a zoomed-out screenshot feels worth sharing.

---

## Phase H — Moderation

**Maps to spec Phase 1 (queue + admin) and Phase 3 (operational use).**
**Done when:** the report menu item works, three distinct reporters auto-hide an observation, the admin endpoint is gated and cascades correctly, and blocked devices write into the void.

### Failing tests to write first

1. `TestReport_WritesToModerationQueue` — POST `/report` with a valid observation id inserts a queue row; response is a small HTML fragment confirming the action.
2. `TestReport_ThreeDistinctDevices_AutoHides` — three reports from three different devices on the same observation → `hidden_at` is set globally; the fourth report is accepted but noop.
3. `TestReport_SameDeviceThreeTimes_DoesNotAutoHide` — reporter identity must be distinct. Prevents self-brigading.
4. `TestAdmin_RequiresTokenFromConfig` — any request without `Authorization: Bearer <token>` returns 401. Wrong token → 401. Right token → 200.
5. `TestAdmin_HideTree_CascadesToObservations` — hiding a tree sets `hidden_at` on the tree and all its observations; subsequent reads exclude all of them.
6. `TestAdmin_Dismiss_OnlyResolvesQueueRow` — no side effects on tree or observation rows.
7. `TestBlockedDevice_WriteSucceeds200_ButInsertedPreHidden` — the write appears to succeed to the client, but the row is `hidden_at = now()` and lives in `moderation_queue` with `resolution = 'hidden'`. Verified by DB state, not response body.

### Exit checklist

- [ ] Admin token is loaded from env var, never hardcoded, never logged. A grep test asserts the token string is not in any rendered response.
- [ ] `/admin/queue` renders usable on a phone at 2am with no script — server-rendered HTML, one `<form>` per queue row.

---

## Phase I — Deploy, observability, backup drill

**Maps to spec Phase 1 exit and Phase 3 stability criteria.**
**Done when:** the production VPS serves real traffic under TLS, `/metrics` exposes the four counters, a backup restore has been rehearsed, and a k6 soak for 1 hour at 50 rps passes without errors.

### Failing tests to write first

1. `TestMetrics_ExposesFourCounters` — `/metrics` returns exactly: `trees_total`, `observations_total`, `devices_total`, `moderation_queue_depth`. No framework-emitted noise, no histogram sprawl.
2. **Deploy smoke test** (runs post-deploy in CI against staging): `/health` 200, `/trees?bbox=...` returns valid GeoJSON, a round-trip pin creation through Playwright succeeds.
3. **Backup-restore drill script** (`scripts/restore-drill.sh`): dump a freshly seeded staging DB to R2, restore into a scratch DB, diff row counts by table, exit non-zero on mismatch. Runs weekly in CI, archived in `docs/notebook.md`.
4. **k6 soak**: 50 rps (mostly reads, 5% writes) for 1 hour against staging; p95 < 200ms, zero 5xx.

### Exit checklist

- [ ] Single-command deploy: `scripts/deploy.sh` = `scp` + `systemctl restart` + smoke test. No orchestrator.
- [ ] Migrations remain manual on prod; the deploy script does **not** run `goose up` (spec invariant).

---

## Phase J — The Finished Craft

**Maps to spec Phase 2 exit.**
**Done when:** every checkbox in spec Phase 2 is a passing test or a recorded manual artifact.

Most test infrastructure exists already from earlier phases. Phase J is primarily about promoting Lighthouse CI, axe-core, and visual-regression suites from "nice to have" to **PR-blocking gates**, and doing the manual passes the spec calls out (screen reader, keyboard-only, 20-iteration style review).

### Exit checklist

- [ ] Lighthouse CI thresholds enforced (95 mobile across all three categories).
- [ ] Axe-core zero serious/critical violations.
- [ ] NVDA + VoiceOver manual transcripts recorded in `docs/accessibility.md`.

---

## Phase K — The Wave (V2)

**Maps to spec Phase 4.**
**Done when:** the post-season release ships a Deck.gl H3HexagonLayer heatmap, a time-lapse of the just-completed season, and the "on this day last year" card.

### Failing tests to write first

1. `TestAggregation_H3R7_ReturnsHexCountsForBbox` — seeded observations produce expected counts per r7 cell. Verifies that H3 columns populated in Phase C are still consistent.
2. `TestArchive_DownloadProducesStableDataset` — the `GET /archive/<year>.json` response for a finalized year is byte-identical across two calls; hidden items excluded; hashed and committed as a golden file per year.
3. **Playwright `heatmap.spec.ts`** — the heatmap toggle reveals a Deck.gl canvas over the MapLibre base; clicking a hex shows an aggregate panel (count of trees, peak-bloom percentage).
4. **Playwright `timelapse.spec.ts`** — scrubbing the time slider advances the visible observations; playback loops the season.
5. `TestOnThisDay_SurfacesCardForReturningDevice` — a device with at least one tree created more than 350 days ago sees the card on visit during the same ISO week; younger devices do not.

### Exit checklist

- [ ] The archive file for year Y is produced once, then is immutable. A test asserts modification of a past year's archive fails.
- [ ] The heatmap is a frontend-only addition; zero new server routes beyond `/archive/*`. Confirmed by route diff.

---

## Phase L — The Garden (V3)

**Maps to spec Phase 5.**
**Done when:** at least two additional species (Nandi Flame, Bougainvillea) live alongside jacaranda with their own bloom calendars and pin styles, without losing the jacaranda's primacy in the UI.

### Failing tests to write first

1. `TestSpecies_JacarandaIsDefaultWhenUnspecified` — a write without a species still goes in as `jacaranda`. Preserves the spec's identity during migration.
2. `TestSpecies_BloomStateValidPerSpecies` — some species may have fewer or different states; the enum per-species is enforced at the DB layer (likely via a check constraint using a mapping table, not a widened enum).
3. `TestFilter_SpeciesSelector` — a user can filter the map to one species; other species pins hide; filter state persists across reloads via URL query param (not cookies).
4. `TestClaimATree_ActivatesAtThreePins` — a device that has placed observations on a single tree three or more times sees a "claim this tree" UI; at two, does not. This is a gentle reward, not a badge; confirm no public signal.

### Exit checklist

- [ ] Jacaranda remains the default species and the default visual identity.
- [ ] Cross-species regressions guarded: Phase C–H tests now parameterized over species in CI.

---

## Phases M+ — Long Tending and Archive

**Map to spec Phases 6 and 7.**

These phases are engineering tending, not engineering building. The test suites above are the scaffolding. What changes is discipline: security patches only, dependency bumps only, archive exports verified once per year. The rituals from `spec.md` — the annual backup drill, the annual spec re-read, the printed screenshots — are the project's long-term tests. Nothing in this document replaces them.

---

## Cross-cutting non-negotiables

These apply from Phase A onwards and are enforced by tests in every suite:

1. **Every schema change has a reversible migration.** Tested by a `goose up && goose down && goose up` loop in CI.
2. **No row is ever mutated in `observations`.** Architectural test + DB-level revoke of UPDATE on the table for the app role.
3. **No handler ever selects rows where `hidden_at IS NOT NULL` for public reads.** A linter-style test greps the repository for `hidden_at` usage and fails if a public query forgets the filter.
4. **Device cookies are UUIDv4.** Test-protected. Do not let someone "optimize" this to UUIDv7.
5. **Trees are deduplicated at 3.0m, not 5.0m.** Test-protected with comments pointing to the spec rationale (Ngong Road spacing).
6. **The data model is four tables.** A `TestSchema_HasExactlyFourAppTables` enumerates them and fails on any addition. Changing this test is a deliberate act that requires spec amendment.
7. **CI runs the full suite on every PR.** No skipped tests without a linked issue and a date.

---

## First ten commits (to make this real)

Before this plan is abstract, these ten commits turn it concrete:

1. `chore: initialize go module, add .editorconfig, Makefile with test/test-integration targets`
2. `test: add failing /health endpoint test`
3. `feat: minimal net/http server satisfying /health`
4. `chore: docker-compose.yml with postgres:16 + postgis + h3-pg, plus MinIO`
5. `test: migration up/down round-trip integration test (failing)`
6. `feat: goose migration 0001 — extensions, trees, observations, devices, moderation_queue with UUIDv7/v4 defaults`
7. `test: bloom_state enum rejects unknown values (failing)`
8. `feat: bloom_state enum (budding, partial, full, fading)`
9. `test: device middleware issues UUIDv4 cookie (failing)`
10. `feat: device middleware + devices repository + upsert on request`

After commit 10, the walking skeleton of Phase A is almost complete. The remaining Phase A tests (empty GeoJSON endpoint, MapLibre shell) follow the same rhythm. The rest of the plan unfolds commit by commit, test-first, until the first jacaranda opens and the map is ready.

---

*This plan is a living document. If it stops describing reality during a build, amend it before writing the next line of code.*
