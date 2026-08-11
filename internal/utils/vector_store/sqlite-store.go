package vector_store

import (
	"bytes"
	"database/sql"
	"encoding/gob"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"

	"github.com/ldaidone/goembedx/pkg/embedx"
	_ "modernc.org/sqlite"
)

// sqliteDBFileName is the database file created inside the store directory.
// The directory is the same per-project path the Badger backend uses, so both
// backends can coexist (though not on the same live data).
const sqliteDBFileName = "vectors.db"

// SQLiteStore implements the vector_store.Store contract on top of a single
// SQLite database file in WAL journal mode. Unlike BadgerDB, SQLite lets
// several processes open the same database concurrently, so multiple MCP
// servers can share one store without the exclusive directory lock.
type SQLiteStore struct {
	db *sql.DB
}

// Compile-time interface checks.
var (
	_ embedx.VectorStore = (*SQLiteStore)(nil)
	_ embedx.Store       = (*SQLiteStore)(nil)
)

// NewSQLiteStore opens (creating if necessary) the vector store backed by the
// SQLite database file dir/vectors.db. dir is created when it does not exist.
// The connection is configured with WAL journaling, a busy timeout, and
// NORMAL synchronous mode so concurrent processes can read and write safely.
func NewSQLiteStore(dir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create sqlite store directory: %w", err)
	}

	path := filepath.Join(dir, sqliteDBFileName)
	dsn := (&url.URL{Scheme: "file", Path: path}).String() +
		"?_journal_mode=WAL&_busy_timeout=10000&_synchronous=NORMAL"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS vectors (id TEXT PRIMARY KEY, data BLOB NOT NULL)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to create vectors table: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// SaveVector stores a vector by ID without associated metadata.
func (s *SQLiteStore) SaveVector(id string, vec []float32) error {
	norm := computeNorm(vec)
	blob, err := encodeVectorData(vectorData{Vector: vec, Norm: norm, Meta: nil})
	if err != nil {
		return err
	}
	return s.upsert(id, blob)
}

// GetVector retrieves the vector stored under id.
func (s *SQLiteStore) GetVector(id string) ([]float32, error) {
	data, err := s.getRaw(id)
	if err != nil {
		return nil, err
	}
	return data.Vector, nil
}

// GetAllVectors returns every stored vector keyed by its ID.
func (s *SQLiteStore) GetAllVectors() (map[string][]float32, error) {
	rows, err := s.db.Query(`SELECT id, data FROM vectors`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	vectors := make(map[string][]float32)
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		data, err := decodeVectorData(blob)
		if err != nil {
			return nil, fmt.Errorf("failed to decode vector %s: %w", id, err)
		}
		vectors[id] = data.Vector
	}
	return vectors, rows.Err()
}

// Close releases the underlying SQLite database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// DeleteStale removes every stored vector whose key is not present in valid,
// returning how many were deleted. Used during graph rebuilds so vectors for
// removed files or entities stop surfacing in semantic search.
func (s *SQLiteStore) DeleteStale(valid map[string]struct{}) (int, error) {
	rows, err := s.db.Query(`SELECT id FROM vectors`)
	if err != nil {
		return 0, err
	}

	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if _, ok := valid[id]; !ok {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	for _, id := range stale {
		if _, err := s.db.Exec(`DELETE FROM vectors WHERE id = ?`, id); err != nil {
			return 0, err
		}
	}
	return len(stale), nil
}

// Add stores a vector with the given ID and associated metadata, precomputing
// the L2 norm for faster similarity calculations.
func (s *SQLiteStore) Add(id string, vec []float32, meta map[string]any) error {
	norm := computeNorm(vec)
	blob, err := encodeVectorData(vectorData{Vector: vec, Norm: norm, Meta: meta})
	if err != nil {
		return err
	}
	return s.upsert(id, blob)
}

// Get retrieves a vector by its ID along with its precomputed norm and metadata.
func (s *SQLiteStore) Get(id string) ([]float32, float32, map[string]any, error) {
	data, err := s.getRaw(id)
	if err != nil {
		return nil, 0, nil, err
	}
	return data.Vector, data.Norm, data.Meta, nil
}

// Search returns the top-k vectors most similar to the query by cosine
// similarity, computed against the stored precomputed norms.
func (s *SQLiteStore) Search(query []float32, k int) ([]embedx.SearchResult, error) {
	results := make([]embedx.SearchResult, 0)
	queryNorm := computeNorm(query)

	rows, err := s.db.Query(`SELECT id, data FROM vectors`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		data, err := decodeVectorData(blob)
		if err != nil {
			return nil, fmt.Errorf("failed to decode vector %s: %w", id, err)
		}

		if len(data.Vector) != len(query) {
			continue
		}

		var dotProduct float32
		for i := range query {
			dotProduct += query[i] * data.Vector[i]
		}

		if queryNorm == 0 || data.Norm == 0 {
			continue
		}

		results = append(results, embedx.SearchResult{
			ID:    id,
			Score: dotProduct / (queryNorm * data.Norm),
			Meta:  data.Meta,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if k > 0 && len(results) > k {
		results = results[:k]
	}
	return results, nil
}

// ImportVectors imports multiple vectors from a map of ID to vector data.
func (s *SQLiteStore) ImportVectors(vectors map[string][]float32) error {
	for id, vec := range vectors {
		if err := s.SaveVector(id, vec); err != nil {
			return fmt.Errorf("failed to import vector %s: %w", id, err)
		}
	}
	return nil
}

// ExportVectors exports all stored vectors to a map of ID to vector data.
func (s *SQLiteStore) ExportVectors() (map[string][]float32, error) {
	return s.GetAllVectors()
}

func (s *SQLiteStore) upsert(id string, blob []byte) error {
	_, err := s.db.Exec(
		`INSERT INTO vectors(id, data) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET data = excluded.data`,
		id, blob,
	)
	return err
}

func (s *SQLiteStore) getRaw(id string) (vectorData, error) {
	var blob []byte
	if err := s.db.QueryRow(`SELECT data FROM vectors WHERE id = ?`, id).Scan(&blob); err != nil {
		return vectorData{}, err
	}
	return decodeVectorData(blob)
}

// encodeVectorData gob-encodes a vector record for storage.
func encodeVectorData(data vectorData) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeVectorData decodes a stored blob, transparently migrating records
// written by the legacy []float32-only format (no precomputed norm).
func decodeVectorData(blob []byte) (vectorData, error) {
	var data vectorData
	dec := gob.NewDecoder(bytes.NewReader(blob))
	if err := dec.Decode(&data); err == nil {
		return data, nil
	} else {
		var legacy []float32
		decOld := gob.NewDecoder(bytes.NewReader(blob))
		if oldErr := decOld.Decode(&legacy); oldErr != nil {
			return vectorData{}, fmt.Errorf("failed to decode vector data: %w", err)
		}
		return vectorData{Vector: legacy, Norm: computeNorm(legacy), Meta: nil}, nil
	}
}

// computeNorm computes the L2 norm of a vector.
func computeNorm(vec []float32) float32 {
	var norm float32
	for _, val := range vec {
		norm += val * val
	}
	return float32(math.Sqrt(float64(norm)))
}
