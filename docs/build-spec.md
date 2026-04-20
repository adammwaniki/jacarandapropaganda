# JacarandaPropaganda — Build Spec

*A living map of Nairobi's jacaranda season. Named in tribute to the #JacarandaPropaganda hashtag that the city's people already share each October, and in quiet protest against the disappearance of the trees that inspired it.*

---

## Summary

JacarandaPropaganda is a private-first, crowd-sourced map of Nairobi's jacaranda trees and their bloom states, updated in real time by the people who walk beneath them. Each tree is a pin; each pin carries a photo and a bloom stage (bud, peak, dropping); the map as a whole becomes a living portrait of the city's September–November bloom. No accounts, no feeds, no likes, no streaks. The map itself is the product, and the screenshot of the map is the marketing.

## User buy-in

### "I love jacaranda season but it's over before I plan around it, and the trees that made my city beautiful are being cut down faster than I can notice."

Users show up because the bloom is brief, beloved, and fading. They stay because the app gives them a way to pay attention — to a tree, a street, a season — without the cost of performance. They return because their pins compound quietly into a record of the city they loved, and because each October the map fills up again like an old friend returning.

The app does not ask users to be creators, influencers, or contributors to a platform. It asks them to notice, and it honors their noticing.

## User journey

1. User opens the PWA on a purple morning. Map of Nairobi, already populated with trees pinned by others, loads in under two seconds.
2. User sees a jacaranda in peak bloom on their street. Taps the floating "+" button.
3. Phone asks to use camera and location. User accepts. Photograph taken. GPS captured.
4. User picks one of three buttons: **Bud**, **Peak**, **Dropping**. Done. Pin lives on the map.
5. Later, user taps an existing pin someone else placed. Sees the current photo, the current bloom state, when it was last updated. Optionally updates the bloom state from their own observation.
6. Next October, the user returns. The app quietly shows a card: "On this week last year, you pinned this tree." The pin is still there. The tree may or may not be.

Total time-to-pin: under 20 seconds. Total in-app time expected per session: under two minutes. The app's goal is to be used briefly and gratefully, not often and anxiously.

## App journey

**V1 — First Bloom (launch, late August before the first jacarandas open).**
Single species. Pin a tree, tag its state, upload a photo, see the city-wide map. Muted base layer, custom purple pins, screenshot-worthy by default. No accounts, no notifications, no filters beyond bloom state.

**V2 — The Wave (post-bloom, December–January following launch).**
A heatmap layer aggregating bloom density across H3 hexagonal cells. A time-lapse view showing the bloom wave moving across Nairobi during the past season. An "on this day last year" gentle resurface. This is the artifact of the season, built from the season's data.

**V3 — The Garden (year two onward).**
Additional species on their own calendars: Nandi Flame, Bottlebrush, Bougainvillea, Peltophorum. A claim-a-tree feature for trees a user has pinned three or more times. Multi-year bloom comparisons. Optional: a printed annual zine.

**V∞ — The Archive.**
If the project outlives its maker, it becomes a permanent record of Nairobi's bloom cycles during a period of climate shift and urban expansion. A dataset for ecologists, an elegy for trees felled to road-widening, a quiet civic memory.

## Feedback loop

Pin → other users see it on the map → they visit the tree or notice it on their commute → they update its bloom state from their own observation → the pin's state reflects the city's current truth rather than one person's out-of-date report. The act of measuring makes users notice trees they had walked past for years; the act of using the map gives them reasons to walk new streets. Both sides of the loop feel good, and neither depends on social reward.

The map is self-correcting through use, dormant out-of-season, and self-amplifying during peak bloom. Dormancy is not a flaw to fix. It is part of the rhythm the trees themselves keep.

---

## Architecture

JacarandaPropaganda is designed as a single deployable unit: one Go binary, one Postgres database, one object storage bucket, one CDN. The architecture prizes legibility, longevity, and cheap operation over fashionable layering. A solo maintainer should be able to read the whole system in a weekend.

### Overview

```txt
                                 ┌──────────────┐
                                 │  Cloudflare  │
                                 │     (CDN)    │
                                 └──────┬───────┘
                                        │
                  ┌─────────────────────┴─────────────────────┐
                  │                                           │
                  ▼                                           ▼
         ┌────────────────┐                          ┌─────────────────┐
         │   Go binary    │                          │  Cloudflare R2  │
         │   (VPS)        │                          │  - photos       │
         │                │                          │  - PMTiles      │
         │  net/http      │                          │  - static assets│
         │  html/template │                          └─────────────────┘
         │  pgx           │
         └────────┬───────┘
                  │
                  ▼
         ┌─────────────────┐
         │   PostgreSQL    │
         │   + PostGIS     │
         │   + h3-pg       │
         └─────────────────┘
```

### Request flow

The browser loads a server-rendered HTML shell from the Go binary. The shell embeds Alpine.js (for reactive UI state), Alpine AJAX (for HTML fragment swaps on server interactions), and MapLibre GL JS (for the map canvas). The map loads vector tiles directly from a PMTiles file hosted on R2 — no intermediate tile server. Pin data is fetched from the Go server as GeoJSON, filtered by the current map viewport. User interactions (create pin, update bloom state) post to Go handlers that return HTML fragments, which Alpine AJAX swaps into the DOM. Photo uploads bypass the Go server entirely: the server issues a signed URL, the browser uploads directly to R2.

In V2, Deck.gl is layered on top of MapLibre for the bloom-density heatmap, sharing view state with the base map.

### Data model

Four tables. The model's virtue is still what it omits; the fourth exists because October will demand it.

**`trees`** — permanent records of tree locations.
Columns: `id` (uuid, **UUIDv7** — time-ordered for index locality), `location` (PostGIS geography point, SRID 4326), `h3_cell_r9` (bigint, indexed — fast proximity queries), `h3_cell_r7` (bigint, indexed — heatmap aggregation), `species` (text, default `'jacaranda'`), `created_at` (timestamptz), `created_by_device` (uuid), `hidden_at` (timestamptz, nullable — soft-delete for moderation).

**`observations`** — the flowing river of bloom-state reports.
Columns: `id` (uuid, **UUIDv7**), `tree_id` (uuid, fk), `bloom_state` (enum: `bud`, `peak`, `dropping`), `photo_r2_key` (text, nullable), `observed_at` (timestamptz), `reported_by_device` (uuid), `hidden_at` (timestamptz, nullable). A tree's current state is the most recent non-hidden observation. Historical observations are preserved forever — they are the archive.

**`devices`** — minimal identity for rate-limiting and gentle "your pins" highlighting.
Columns: `id` (uuid, **UUIDv4** — deliberately random so first-visit timestamps do not leak through the cookie), `first_seen` (timestamptz), `last_seen` (timestamptz), `blocked_at` (timestamptz, nullable).

**`moderation_queue`** — the one table added for operational sanity, not product ambition.
Columns: `id` (uuid, UUIDv7), `target_kind` (enum: `tree`, `observation`), `target_id` (uuid), `reason` (text, nullable — submitted by reporter), `reporter_device` (uuid), `created_at` (timestamptz), `resolved_at` (timestamptz, nullable), `resolution` (enum: `hidden`, `dismissed`, nullable). No public UI around reports; a small "report this photo" link behind an overflow menu writes here. The queue exists to make the admin endpoint usable at 2am on launch weekend. If the queue sits empty for two seasons, reconsider.

**ID strategy:** UUIDv7 is used for public-facing, append-heavy tables so B-tree indexes stay cache-friendly and new rows cluster at the right edge of the index — meaningful at 50k+ rows. UUIDv4 is used for `devices.id` because a device ID lives in a user's cookie, and a time-ordered ID would silently leak first-visit timestamps to anyone inspecting the cookie. Privacy wins over index locality for identity.

No `users` table. No `sessions` table. No `comments`, `likes`, `follows`, or `notifications` tables. If a sixth table ever becomes necessary, sit with the necessity for a week before adding it.

### Tree identity and deduplication

Tree identity is anchored to exact coordinates, not to H3 cells. H3 is a tessellation used for aggregation and proximity, not an identity system — two branches of the same jacaranda can fall into different H3 r15 cells depending on GPS noise, and two trees on opposite sides of a street can fall into the same r15 cell.

Deduplication happens at write time, with a UI that lets the user *see* rather than *guess*. When a user submits a new pin, the server runs `ST_DWithin(existing.location, new.location, 3)` against the `trees` table filtered by `species` and `hidden_at IS NULL`. The radius is 3m, not 5m: on Nairobi's jacaranda-lined streets (Riverside, Ngong Road, Kenyatta Avenue) trees are often 4–6m apart, and a 5m radius fires the prompt against neighbors almost every time.

If any candidates are returned, the user sees a comparison sheet, not a yes/no question: the most recent photo of each candidate tree, its last-observed bloom state, and its distance in meters. Two buttons per candidate: **"This is the same tree"** (becomes an observation on that tree) or a single bottom action **"None of these — pin new tree"** (creates a new row). If the user picks "same tree," their new photo and bloom state are written as an observation on the existing tree; their GPS reading is discarded, so identity stays anchored to the original coordinates.

If no candidates are returned, the pin is created silently with no prompt.

This keeps the map honest without requiring moderation of duplicates, and it refuses to make the user adjudicate under GPS drift.

### Identity

The app uses no accounts. On first visit, the server sets a `device_id` cookie containing a UUIDv4. This cookie is the only identity the app tracks. It is used for:

- Rate-limiting pin creation (see below).
- Rendering the user's own pins with a subtle visual accent.
- Surfacing "on this day last year" cards during the user's next bloom season.

If a user clears their cookies, they become a new user. This is not a bug. It is a correct model of a private, ephemeral relationship with a tool.

### Rate limiting and abuse

A cookie-only limit is trivially bypassed and was worth naming honestly. V1 uses a two-signal limit: **10 new trees per device per rolling 24 hours, and 30 new trees per IP per rolling 24 hours.** Observations on existing trees are limited separately and more loosely (60 per device per day) because adding an observation to a confirmed tree is a lower-risk action than placing a new pin.

These numbers are tight enough that an enthusiastic user walking their neighborhood during peak bloom can still pin 10 new trees a day and add observations to dozens more, and loose enough that a single bad actor cannot paint the map in an afternoon. If a genuine user hits the limit, the UI says so plainly and invites them back tomorrow. The limits are stored in Postgres (a small counter table, pruned nightly), not Redis — see the caching section.

Abuse beyond rate limits is handled by the moderation path below.

### Moderation

The spec previously claimed no moderation story. October will not allow that.

The moderation surface is deliberately small:

1. Every photo viewer has a small overflow menu with a single action: **"Report this photo"**. Tapping writes a row to `moderation_queue` and optimistically hides the observation for the reporting device only.
2. Three reports from distinct devices on the same observation auto-hide it globally (sets `hidden_at`) and flag it in the queue for review.
3. A single admin endpoint (`/admin/queue`, behind a long random token in the binary's config) lists queued items with their photos and lets the operator choose **hide** or **dismiss**. Hiding an `observation` sets `hidden_at`; hiding a `tree` sets `hidden_at` on the tree *and* cascades to its observations. Dismissing marks the queue row resolved and does nothing else.
4. A `devices.blocked_at` flag exists for the rare case where one device is a repeat bad actor; blocked devices can read the map but their writes return 200 and are silently dropped into the queue as pre-hidden.

No public UI exposes report counts, moderator identity, or queue depth. The operator is a person, not a feature. Expected load is a handful of items per week, spiking during press moments; if it ever exceeds an hour of work per day, the project has a different problem than this spec anticipates.

### Media handling

Photos are compressed client-side to a maximum of 500KB before upload. The Go server issues a presigned R2 upload URL scoped to the specific object key, with a **Content-Length** ceiling of 1MB and a **Content-Type** restricted to `image/jpeg` and `image/webp` enforced in the presigned policy. The server never touches photo bytes, but the ceiling means a malicious client cannot PUT a 500MB file directly to the bucket.

Server-side: on successful upload notification (optional R2 event, otherwise lazy verification on first read), the server issues a HEAD to confirm size and content-type match the policy. Mismatches flag the observation into `moderation_queue` as pre-hidden.

Photo URLs served to clients are R2 public URLs through the Cloudflare CDN. No image resizing pipeline in V1 — the 500KB client-side cap is the pipeline.

### Proximity and aggregation

Two geographic indexes coexist by design:

- **PostGIS geography** on `trees.location` for exact distance queries, viewport filtering, and the 3-meter deduplication check at write time. Used by `GET /trees?bbox=...`.
- **H3 cells** at resolution 9 (~175m edge) and resolution 7 (~1.4km edge) for fast bucketed queries and the V2 heatmap layer. H3 r9 powers "nearby trees" without a spatial join; H3 r7 powers the Deck.gl H3HexagonLayer visualization.

Finer H3 resolutions (r11, r13, r15) were considered and deliberately excluded. H3 is a tessellation, not a coordinate system — at fine resolutions it creates arbitrary cell boundaries that split single trees and merge neighboring ones based on GPS noise. Tree identity is the job of exact coordinates plus the deduplication rule; H3 is the job of aggregation.

For V1, PostGIS alone is sufficient. H3 columns are populated from day one so V2's heatmap is a pure frontend addition, not a data migration.

### Caching strategy

No Redis, no Memcached, no application-level cache layer in V1 or V2. This is a deliberate choice, not a deferral.

At expected scale (50k trees, 100k observations, viral-spike peak of ~50 requests/second during October press moments), Postgres's built-in shared buffer cache serves hot reads in single-digit milliseconds. Cloudflare caches PMTiles at the edge. GeoJSON viewport queries are fast enough that an application cache would add more operational complexity than it removes latency.

If caching becomes necessary — typically signaled by Postgres CPU sustained above 60% during peak hours — the first step is a Postgres materialized view of current pin states refreshed every 30 seconds, not a new data store. Redis earns its place only if real-time features (websocket presence, live pin notifications) enter the product, which they are not planned to.

Every component not added here is a small tax not paid forever.

### Deployment

A single VPS (Hetzner CX22 or Digital Ocean equivalent, ~€5/month) running:

- The Go binary as a systemd service.
- PostgreSQL with PostGIS and h3-pg extensions.
- Caddy for TLS termination and reverse proxy (or Cloudflare Tunnel as an alternative).

Cloudflare sits in front for CDN, caching, and DDoS protection during the October traffic spike. R2 sits beside for photos and tiles. The entire production system costs less than €15/month at expected scale and can absorb a viral spike of 100k visits in a day without operator intervention.

Migrations run manually on deploy — `goose up` is a separate step after `systemctl restart`, not part of the binary's startup path. This is deliberate for two reasons: it prevents a bad migration from taking down the service during a routine deploy, and it forces the operator to read the migration before applying it. The cost is one extra command on deploy weeks; the benefit is that a schema change is always a conscious act.

### Backups

Nightly `pg_dump` to R2 with 30-day retention, plus a weekly snapshot held for a year. Photos in R2 are implicitly durable (11 nines). The database backup is what matters — it contains the civic memory. Restores should be rehearsed once before launch and once per year thereafter; an untested backup is a wish.

---

## Tech Stack

### Language & server

- Go 1.22+ (stdlib `net/http`, `html/template`)
- `pgx/v5` for Postgres (no ORM)
- `goose` for database migrations
- `aws-sdk-go-v2` for R2 (R2 is S3-compatible)

### Database

- PostgreSQL 16
- PostGIS 3.4+ extension (geography types, spatial indexing)
- `h3-pg` extension (H3 cell operations in SQL)

### Client runtime

- Alpine.js 3.x (reactive UI state)
- Alpine AJAX plugin (HTMX-style HTML fragment swaps over Alpine)
- MapLibre GL JS 4.x (vector map rendering)
- Deck.gl 9.x (V2 only — H3HexagonLayer for heatmap)
- Protomaps `protomaps-leaflet` integration for MapLibre (PMTiles protocol handler)

### Map data

- Protomaps PMTiles single-file vector tiles for Nairobi/Kenya, hosted on R2
- Custom MapLibre style JSON, hand-crafted as the project's visual identity

### Storage & delivery

- Cloudflare R2 (photos, PMTiles, static assets)
- Cloudflare CDN (caching, TLS, DDoS protection)

### Infrastructure

- Single VPS: Hetzner or Digital Ocean, ~€5/month
- Caddy 2.x for TLS and reverse proxy
- systemd for service management
- Docker Compose for local development parity

### Observability

- Structured logs to stdout, rotated by journald
- Uptime Kuma (self-hosted) for uptime monitoring
- A `/metrics` endpoint exposing pin count, observation count, device count, moderation queue depth

### Build & deploy

- GitHub Actions for CI (test, lint, build)
- Single-binary deployment: `scp` to VPS, restart systemd service
- Migrations run manually on deploy (not auto — see Deployment)

### Conspicuously absent, and why

- No JavaScript framework (React, Vue, Svelte). Alpine + server-rendered HTML is sufficient and outlives framework churn.
- No ORM. SQL is the right interface for four tables and a spatial query.
- No Redis or Memcached. Postgres's shared buffer cache and Cloudflare's edge cache are sufficient at expected scale; an application cache layer would add operational surface without measurable benefit.
- No Kubernetes, no Docker Swarm, no service mesh. One binary, one box.
- No user analytics SDK. Pin count is the only metric that matters, and it's in the database.
- No email service. Nothing in V1 sends email.
- No auth provider. There are no accounts to authenticate. The single admin endpoint is gated by a static token in config, not by a login.
- No public moderation UI. Reports are a single menu item; the queue is for the operator.

Every item on this absent list is a choice to reduce surface area so the project can be maintained by one person across many years, quietly, while most of the stack continues to work.

---

## Rituals

*The things that are not engineering tasks but belong in the spec because the spec would be dishonest without them.*

- Walk at least three Nairobi streets known for jacarandas before launch, while the trees are still green, so you know them before they bloom.
- Before each season, re-read this spec and the previous season's notebook. Edit the spec where reality disagrees with it. Edits are the spec working, not failing.
- Keep a paper notebook during each bloom. No acting on it during the season. Transcribe it to a markdown file in the repo between December and February.
- Once a year, restore the most recent backup into a scratch database and confirm it works.
- On the first peak-bloom day of each season, take a screenshot of the map. Print it. Keep the prints.

---

## Phased Objectives

Phases are measured by what becomes true when they complete, not by elapsed weeks. Each phase produces something whole — something you could, in principle, stop at and still have offered a gift.

### Phase 0 — Readiness

*The project exists in the world, even if only as a signal of intent.*

**Completed when:**

- The domain `jacarandapropaganda.org` (or equivalent) resolves to a holding page with a single sentence, the name, and an email signup.
- A local development environment runs the Go server, Postgres, and a blank MapLibre map centered on Nairobi.
- The PMTiles file for Nairobi has been generated and uploaded to R2 and successfully loads in MapLibre on a real mobile device over cellular data.
- A git repository exists, committed and backed up.

**Progress indicator:** A phone on Mombasa Road loads your empty map in under three seconds.

### Phase 1 — The Core Offering

*A stranger can pin a tree and the world contains a map that did not exist before.*

**Completed when:**

- A user can open the site on a phone, tap "+", capture a photo, select a bloom state, and see their pin appear on the map — end to end, in under 20 seconds.
- The deduplication comparison sheet works: submitting a pin within 3m of an existing tree shows that tree's photo and bloom state for visual comparison, not a text prompt.
- Other users viewing the site see that pin on their map within seconds of it being created.
- A user can tap any existing pin and update its bloom state, writing a new observation to the database.
- The map has a draft version of its custom visual style: muted base, purple pins, distinguishable bloom states.
- The app works on iOS Safari and Android Chrome. Camera and geolocation permissions flow correctly.
- A single deployable Go binary runs on the production VPS with TLS and serves real traffic.
- The moderation queue table exists, the report menu item works, and the admin endpoint can hide items. Tested with a deliberate bad pin submitted by the operator.

**Progress indicator:** You can send the URL to one friend who has never seen it, and they can pin a tree on their street without any instructions from you.

### Phase 2 — The Finished Craft

*The app is no longer being built. It is being refined.*

**Completed when:**

- The map style has been iterated at least twenty times and is something you would be proud to have signed.
- Custom SVG pins exist for each bloom state and behave beautifully at every zoom level from city-wide to street.
- A subtle stats bar shows live counts of trees, peak-bloom percentage, and last-updated time.
- Filter toggles for bloom state work smoothly with no server round-trip.
- The app passes a keyboard-only navigation test and a screen reader test.
- Photos are client-side compressed and upload reliably on slow connections.
- Presigned upload URLs enforce content-length and content-type; a script attempting to upload a large non-image file fails.
- The site scores 95+ on mobile Lighthouse for performance, accessibility, and best practices.
- A private soft-launch to twenty carefully chosen people has seeded the map with at least 200 real tree pins across Nairobi.

**Progress indicator:** You take a screenshot of the map zoomed out over Nairobi, and you want to share it with strangers without being asked.

### Phase 3 — The First Bloom

*The app meets the season it was made for.*

#### Completed when

- The public launch has happened, quietly, in the week before the first jacarandas open — typically late August or the first days of September.
- At least five Kenyan publications, blogs, or widely-followed accounts have been personally contacted with a preview link and a short, honest pitch.
- Throughout September, October, and November, the app is stable. No unplanned downtime longer than an hour. Photo uploads work. Pins are being created by strangers without your intervention.
- The moderation queue is checked at least once a day during September–November. Median report-to-resolution time is under 12 hours.
- A notebook is being kept, by hand, of every feature request, every moment of delight, every bug report, every message from a user. It is not being acted on during the bloom. It is being collected.
- At peak bloom, the map shows a dense enough constellation of purple pins that a screenshot of it is beautiful on its own terms.
- You have walked under jacarandas during this bloom with more attention than you would have had without the app.

**Progress indicator:** Someone you do not know posts a screenshot of the map to social media without being prompted.

### Phase 4 — The Wave

*The first bloom is archived. The app returns in a new form.*

#### Completed when

- A post-season release (December or January) adds the Deck.gl H3HexagonLayer heatmap showing bloom density across the completed season.
- A time-lapse view replays the bloom wave moving across Nairobi across the September–November period using the accumulated `observations` history.
- The "on this day last year" gentle resurface is implemented, ready for the next bloom.
- The previous season's data is archived permanently — downloadable as a single dataset, never deleted, never silently modified. Hidden items are excluded from the public archive but preserved in backups.
- A second, smaller press cycle has happened with the heatmap as the hook.

**Progress indicator:** The app has two distinct reasons to exist across the calendar year — the bloom itself and the retrospective — and both are beautiful.

### Phase 5 — The Garden

*Other blooms take their places in the map's rhythm.*

#### Completed when:

- At least two additional species — plausibly Nandi Flame and Bougainvillea — have been added with their own bloom calendars, pin styles, and seasonal press moments.
- The codebase's assumptions about "bloom state" and "season" have been generalized without losing the jacaranda's primacy in the app's identity.
- The claim-a-tree feature exists, if the notebook from Phase 3 and Phase 4 confirmed users wanted it.
- A Pretoria or Harare user has asked for the app in their city, and you have decided, with care, whether to say yes.

**Progress indicator:** The app has survived two full jacaranda seasons and still feels like it was made with the same hands that started it.

### Phase 6 — The Long Tending

*The app is older than most apps become. It is tended, not grown.*

#### Completed when:

- The app has been running for five or more jacaranda seasons.
- The codebase has been updated for security but not bloated with features. The data model is still four tables.
- An ecologist, a journalist, or a planner has used the accumulated data for something that mattered outside the app — a research paper, an article, a civic argument about tree preservation.
- The project has a named successor or co-maintainer, so its continuity does not depend on one person's attention.
- When you look at the map, you recognize trees that no longer exist, pinned by people who may no longer live in Nairobi, and the map honors them.

**Progress indicator:** Someone writes about JacarandaPropaganda as if it had always been there.

### Phase 7 — The Archive

*The app becomes history, gracefully.*

#### Completed when:

- The live service, whenever it eventually ends, ends with notice and dignity — not with a sudden 404.
- The full dataset is deposited with a Kenyan cultural institution, a university herbarium, or a civic archive. The photos, the pins, the observations, the timestamps. All of it.
- A final zine, PDF, or printed volume summarizes what was mapped, when, by how many people, across how many seasons.
- The domain redirects to the archive. The code is released under a permissive license on a public repository.
- The trees that were pinned and then cut down are remembered in a section of the archive titled simply: *No longer here.*

**Progress indicator:** A child in Nairobi, thirty years from now, finds a record of the tree that used to stand outside their grandmother's house, because someone pinned it before it was felled.

---

*This spec is a living document. It will change as the app meets reality. But the spine — restraint, attention, the bloom as the teacher — does not change.*
