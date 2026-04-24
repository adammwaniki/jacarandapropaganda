package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/adammwaniki/jacarandapropaganda/internal/geo"
)

// Tree is the Go projection of a row in the trees table. Location is held
// as two floats here rather than a PostGIS wire type because (a) we never
// do spatial math in Go, only in the database, and (b) keeping floats
// simplifies JSON and test comparisons.
type Tree struct {
	ID              uuid.UUID
	Lat             float64
	Lng             float64
	H3R9            int64
	H3R7            int64
	Species         string
	CreatedAt       time.Time
	CreatedByDevice uuid.UUID
	HiddenAt        *time.Time
}

// Observation mirrors the observations table.
type Observation struct {
	ID               uuid.UUID
	TreeID           uuid.UUID
	BloomState       BloomState
	PhotoR2Key       *string
	ObservedAt       time.Time
	ReportedByDevice uuid.UUID
	HiddenAt         *time.Time
}

// TreeWithState is a tree joined with its most recent visible observation.
// The observation may be nil if none has been recorded yet, which is a
// legal transient state between tree insertion and its first observation
// within the same transaction (see InsertWithObservation downstream).
type TreeWithState struct {
	Tree   Tree
	Latest *Observation
}

// CandidateTree is a dedup match: a visible, same-species tree within the
// dedup radius, plus its distance in meters and most-recent observation
// for the comparison sheet the user sees.
type CandidateTree struct {
	TreeWithState
	DistanceMeters float64
}

// InsertTreeParams is the write-path input for creating a new tree. Kept
// as a struct so call sites name their arguments instead of relying on
// positional order — (lat, lng) vs (lng, lat) swaps have sunk map features
// elsewhere, and this project does not have time for that bug.
type InsertTreeParams struct {
	ID        uuid.UUID // UUIDv7
	Lat, Lng  float64
	Species   string
	CreatedBy uuid.UUID // device id
}

// TreeStore is the repository for the trees table. No mocks — tests run
// against real PostGIS + h3-pg.
type TreeStore struct {
	db *sql.DB
}

func NewTreeStore(db *sql.DB) *TreeStore {
	return &TreeStore{db: db}
}

// Insert writes a new tree row and populates H3 r9/r7 cells from the
// location. Rejects non-UUIDv7 ids because the index-locality benefit only
// holds if every row uses v7.
func (s *TreeStore) Insert(ctx context.Context, p InsertTreeParams) error {
	if err := validateInsertTreeParams(p); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, insertTreeSQL, p.ID, p.Lat, p.Lng, p.Species, p.CreatedBy); err != nil {
		return fmt.Errorf("insert tree: %w", err)
	}
	return nil
}

// InsertWithObservation creates a tree row and its first observation in a
// single transaction. If either insert fails, nothing persists — callers
// never have to deal with an orphan tree that has no bloom state.
//
// The observation's TreeID must match the tree's ID. Mismatches are a
// programmer bug; callers should construct the observation params from the
// tree params at the call site.
func (s *TreeStore) InsertWithObservation(ctx context.Context, tp InsertTreeParams, op InsertObservationParams) error {
	if err := validateInsertTreeParams(tp); err != nil {
		return err
	}
	if op.TreeID != tp.ID {
		return fmt.Errorf("store: observation.TreeID %v must match tree.ID %v", op.TreeID, tp.ID)
	}
	if op.ID.Version() != 7 {
		return fmt.Errorf("store: observation id must be UUIDv7, got v%d", op.ID.Version())
	}
	if !op.BloomState.Valid() {
		return fmt.Errorf("store: invalid bloom_state %q", op.BloomState)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, insertTreeSQL, tp.ID, tp.Lat, tp.Lng, tp.Species, tp.CreatedBy); err != nil {
		return fmt.Errorf("insert tree: %w", err)
	}
	if _, err := tx.ExecContext(ctx, insertObservationSQL,
		op.ID, op.TreeID, string(op.BloomState), op.PhotoR2Key, op.ReportedBy,
	); err != nil {
		return fmt.Errorf("insert observation: %w", err)
	}
	return tx.Commit()
}

const insertTreeSQL = `
	INSERT INTO trees (id, location, h3_cell_r9, h3_cell_r7, species, created_by_device)
	VALUES (
		$1,
		ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography,
		h3_lat_lng_to_cell(ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography, 9)::bigint,
		h3_lat_lng_to_cell(ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography, 7)::bigint,
		$4,
		$5
	)
`

func validateInsertTreeParams(p InsertTreeParams) error {
	if p.ID.Version() != 7 {
		return fmt.Errorf("store: tree id must be UUIDv7, got v%d", p.ID.Version())
	}
	if p.Species == "" {
		return errors.New("store: species is required")
	}
	if p.Lat < -90 || p.Lat > 90 {
		return fmt.Errorf("store: lat out of range: %v", p.Lat)
	}
	if p.Lng < -180 || p.Lng > 180 {
		return fmt.Errorf("store: lng out of range: %v", p.Lng)
	}
	return nil
}

// ByID returns a tree by id, or ErrNotFound.
func (s *TreeStore) ByID(ctx context.Context, treeID uuid.UUID) (Tree, error) {
	const q = `
		SELECT id,
		       ST_Y(location::geometry) AS lat,
		       ST_X(location::geometry) AS lng,
		       h3_cell_r9, h3_cell_r7,
		       species, created_at, created_by_device, hidden_at
		FROM trees
		WHERE id = $1
	`
	var t Tree
	err := s.db.QueryRowContext(ctx, q, treeID).Scan(
		&t.ID, &t.Lat, &t.Lng, &t.H3R9, &t.H3R7,
		&t.Species, &t.CreatedAt, &t.CreatedByDevice, &t.HiddenAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Tree{}, ErrNotFound
	}
	if err != nil {
		return Tree{}, fmt.Errorf("by id: %w", err)
	}
	return t, nil
}

// Hide soft-deletes a tree by setting hidden_at. Cascades to its
// observations in the same transaction — hiding a tree must take its
// photos off the map too, and doing both at once guarantees no observer
// of the queue sees the tree gone but its observations still visible.
func (s *TreeStore) Hide(ctx context.Context, treeID uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`UPDATE trees SET hidden_at = now() WHERE id = $1 AND hidden_at IS NULL`, treeID)
	if err != nil {
		return fmt.Errorf("hide tree: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("hide tree: rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE observations SET hidden_at = now() WHERE tree_id = $1 AND hidden_at IS NULL`,
		treeID); err != nil {
		return fmt.Errorf("hide observations: %w", err)
	}
	return tx.Commit()
}

// Candidates returns visible, same-species trees within radiusMeters of
// (lat, lng), each joined with its most recent visible observation and its
// distance in meters. Used by the dedup flow at write time.
func (s *TreeStore) Candidates(ctx context.Context, lat, lng float64,
	species string, radiusMeters float64) ([]CandidateTree, error) {
	const q = `
		SELECT t.id,
		       ST_Y(t.location::geometry) AS lat,
		       ST_X(t.location::geometry) AS lng,
		       t.h3_cell_r9, t.h3_cell_r7,
		       t.species, t.created_at, t.created_by_device, t.hidden_at,
		       ST_Distance(t.location, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography) AS dist,
		       o.id,
		       o.bloom_state,
		       o.photo_r2_key,
		       o.observed_at,
		       o.reported_by_device
		FROM trees t
		LEFT JOIN LATERAL (
			SELECT id, bloom_state, photo_r2_key, observed_at, reported_by_device
			FROM observations
			WHERE tree_id = t.id AND hidden_at IS NULL
			ORDER BY observed_at DESC
			LIMIT 1
		) o ON TRUE
		WHERE t.hidden_at IS NULL
		  AND t.species = $3
		  AND ST_DWithin(
		        t.location,
		        ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography,
		        $4)
		ORDER BY dist ASC
	`
	rows, err := s.db.QueryContext(ctx, q, lat, lng, species, radiusMeters)
	if err != nil {
		return nil, fmt.Errorf("candidates: %w", err)
	}
	defer rows.Close()
	return scanCandidates(rows)
}

// ByBbox returns visible trees whose location falls inside the given
// bounding box, each enriched with its most recent visible observation.
// Ordered by most recently observed so rendering feels "alive".
func (s *TreeStore) ByBbox(ctx context.Context, b geo.Bbox) ([]TreeWithState, error) {
	const q = `
		SELECT t.id,
		       ST_Y(t.location::geometry) AS lat,
		       ST_X(t.location::geometry) AS lng,
		       t.h3_cell_r9, t.h3_cell_r7,
		       t.species, t.created_at, t.created_by_device, t.hidden_at,
		       o.id, o.bloom_state, o.photo_r2_key, o.observed_at, o.reported_by_device
		FROM trees t
		LEFT JOIN LATERAL (
			SELECT id, bloom_state, photo_r2_key, observed_at, reported_by_device
			FROM observations
			WHERE tree_id = t.id AND hidden_at IS NULL
			ORDER BY observed_at DESC
			LIMIT 1
		) o ON TRUE
		WHERE t.hidden_at IS NULL
		  AND ST_Intersects(
		        t.location,
		        ST_MakeEnvelope($1, $2, $3, $4, 4326)::geography)
		ORDER BY COALESCE(o.observed_at, t.created_at) DESC
	`
	rows, err := s.db.QueryContext(ctx, q, b.MinLon, b.MinLat, b.MaxLon, b.MaxLat)
	if err != nil {
		return nil, fmt.Errorf("by bbox: %w", err)
	}
	defer rows.Close()

	out := []TreeWithState{}
	for rows.Next() {
		tw, _, err := scanTreeWithStateAndMaybeDist(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, tw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("by bbox: rows: %w", err)
	}
	return out, nil
}

// --- scanners (kept close together so column ordering stays lockstep) ---

func scanCandidates(rows *sql.Rows) ([]CandidateTree, error) {
	out := []CandidateTree{}
	for rows.Next() {
		tw, dist, err := scanTreeWithStateAndMaybeDist(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, CandidateTree{TreeWithState: tw, DistanceMeters: dist})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("candidates: rows: %w", err)
	}
	return out, nil
}

func scanTreeWithStateAndMaybeDist(rows *sql.Rows, hasDist bool) (TreeWithState, float64, error) {
	var (
		t                Tree
		dist             sql.NullFloat64
		obsID            uuid.NullUUID
		bloom            sql.NullString
		photoKey         sql.NullString
		observedAt       sql.NullTime
		reportedByDevice uuid.NullUUID
	)
	var scanArgs []any
	scanArgs = append(scanArgs,
		&t.ID, &t.Lat, &t.Lng, &t.H3R9, &t.H3R7,
		&t.Species, &t.CreatedAt, &t.CreatedByDevice, &t.HiddenAt,
	)
	if hasDist {
		scanArgs = append(scanArgs, &dist)
	}
	scanArgs = append(scanArgs, &obsID, &bloom, &photoKey, &observedAt, &reportedByDevice)

	if err := rows.Scan(scanArgs...); err != nil {
		return TreeWithState{}, 0, fmt.Errorf("scan: %w", err)
	}
	tw := TreeWithState{Tree: t}
	if obsID.Valid {
		obs := &Observation{
			ID:               obsID.UUID,
			TreeID:           t.ID,
			BloomState:       BloomState(bloom.String),
			ObservedAt:       observedAt.Time,
			ReportedByDevice: reportedByDevice.UUID,
		}
		if photoKey.Valid {
			obs.PhotoR2Key = &photoKey.String
		}
		tw.Latest = obs
	}
	return tw, dist.Float64, nil
}
