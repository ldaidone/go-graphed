package vector_store

import (
	"bytes"
	"encoding/gob"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dgraph-io/badger/v4"
	"github.com/ldaidone/goembedx/pkg/embedx"
)

// newTestStore opens a BadgerStore in a temp dir and registers cleanup.
func newTestStore(t *testing.T) *BadgerStore {
	t.Helper()
	s, err := NewBadgerStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBadgerStore returned error: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSaveAndGetVector(t *testing.T) {
	s := newTestStore(t)
	vec := []float32{1.0, 2.0, 3.0}

	if err := s.SaveVector("doc-1", vec); err != nil {
		t.Fatalf("SaveVector returned error: %v", err)
	}

	got, err := s.GetVector("doc-1")
	if err != nil {
		t.Fatalf("GetVector returned error: %v", err)
	}
	if !reflect.DeepEqual(got, vec) {
		t.Errorf("GetVector = %v, want %v", got, vec)
	}
}

func TestGetVector_NotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetVector("missing"); err == nil {
		t.Error("GetVector on missing id should return an error")
	}
}

func TestAddAndGetWithMeta(t *testing.T) {
	s := newTestStore(t)
	vec := []float32{0.5, -0.5, 1.0}
	meta := map[string]any{"file": "a.go", "kind": "struct"}

	if err := s.Add("a.go#Foo", vec, meta); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	gotVec, gotNorm, gotMeta, err := s.Get("a.go#Foo")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	if !reflect.DeepEqual(gotVec, vec) {
		t.Errorf("Get vector = %v, want %v", gotVec, vec)
	}
	if !reflect.DeepEqual(gotMeta, meta) {
		t.Errorf("Get meta = %v, want %v", gotMeta, meta)
	}

	wantNorm := float32(math.Sqrt(0.25 + 0.25 + 1.0))
	if math.Abs(float64(gotNorm-wantNorm)) > 1e-6 {
		t.Errorf("Get norm = %v, want %v", gotNorm, wantNorm)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := newTestStore(t)
	if _, _, _, err := s.Get("missing"); err == nil {
		t.Error("Get on missing id should return an error")
	}
}

func TestGetAllVectors(t *testing.T) {
	s := newTestStore(t)
	vecs := map[string][]float32{
		"a": {1, 0, 0},
		"b": {0, 1, 0},
		"c": {0, 0, 1},
	}
	for id, vec := range vecs {
		if err := s.SaveVector(id, vec); err != nil {
			t.Fatalf("SaveVector(%s) returned error: %v", id, err)
		}
	}

	all, err := s.GetAllVectors()
	if err != nil {
		t.Fatalf("GetAllVectors returned error: %v", err)
	}
	if len(all) != len(vecs) {
		t.Errorf("GetAllVectors count = %d, want %d", len(all), len(vecs))
	}
	for id, vec := range vecs {
		if !reflect.DeepEqual(all[id], vec) {
			t.Errorf("GetAllVectors[%s] = %v, want %v", id, all[id], vec)
		}
	}
}

func TestSearch_TopKOrdering(t *testing.T) {
	s := newTestStore(t)
	// Store axis-aligned unit vectors; query (1,0,0) is most similar to "x".
	vecs := map[string][]float32{
		"x": {1, 0, 0},
		"y": {0, 1, 0},
		"z": {0, 0, 1},
	}
	for id, vec := range vecs {
		if err := s.Add(id, vec, map[string]any{"label": id}); err != nil {
			t.Fatalf("Add(%s) returned error: %v", id, err)
		}
	}

	results, err := s.Search([]float32{1, 0, 0}, 3)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Search returned %d results, want 3", len(results))
	}
	if results[0].ID != "x" {
		t.Errorf("top result = %q, want %q", results[0].ID, "x")
	}
	if results[0].Meta["label"] != "x" {
		t.Errorf("top result meta = %v, want label x", results[0].Meta)
	}

	// Top-1 truncation
	top1, err := s.Search([]float32{1, 0, 0}, 1)
	if err != nil {
		t.Fatalf("Search(k=1) returned error: %v", err)
	}
	if len(top1) != 1 || top1[0].ID != "x" {
		t.Errorf("Search(k=1) = %+v, want single result x", top1)
	}
}

func TestSearch_ExcludesDimensionMismatch(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveVector("dim3", []float32{1, 2, 3}); err != nil {
		t.Fatalf("SaveVector returned error: %v", err)
	}
	if err := s.SaveVector("dim4", []float32{1, 2, 3, 4}); err != nil {
		t.Fatalf("SaveVector returned error: %v", err)
	}

	results, err := s.Search([]float32{1, 2, 3}, 10)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search returned %d results, want 1", len(results))
	}
	if results[0].ID != "dim3" {
		t.Errorf("result ID = %q, want %q", results[0].ID, "dim3")
	}
}

func TestImportExportVectors(t *testing.T) {
	s := newTestStore(t)
	vecs := map[string][]float32{
		"pkg/a.go#A": {1, 0, 0},
		"pkg/b.go#B": {0, 1, 0},
	}

	if err := s.ImportVectors(vecs); err != nil {
		t.Fatalf("ImportVectors returned error: %v", err)
	}

	exported, err := s.ExportVectors()
	if err != nil {
		t.Fatalf("ExportVectors returned error: %v", err)
	}
	if len(exported) != len(vecs) {
		t.Errorf("ExportVectors count = %d, want %d", len(exported), len(vecs))
	}
	for id, vec := range vecs {
		if !reflect.DeepEqual(exported[id], vec) {
			t.Errorf("ExportVectors[%s] = %v, want %v", id, exported[id], vec)
		}
	}
}

func TestBadgerStore_ImplementsInterfaces(t *testing.T) {
	var _ embedx.VectorStore = (*BadgerStore)(nil)
	var _ embedx.Store = (*BadgerStore)(nil)
}

// TestBackwardCompatibility migrates records stored in the old []float32 gob
// format (no precomputed norm) when read through GetVector/Get.
func TestBackwardCompatibility(t *testing.T) {
	s := newTestStore(t)

	// Simulate a legacy record: gob-encoded []float32 written directly to the DB.
	legacy := []float32{3, 4} // norm should be 5
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(legacy); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("legacy-id"), buf.Bytes())
	}); err != nil {
		t.Fatalf("failed to write legacy record: %v", err)
	}

	vec, err := s.GetVector("legacy-id")
	if err != nil {
		t.Fatalf("GetVector on legacy record returned error: %v", err)
	}
	if !reflect.DeepEqual(vec, legacy) {
		t.Errorf("GetVector legacy = %v, want %v", vec, legacy)
	}

	gotVec, gotNorm, _, err := s.Get("legacy-id")
	if err != nil {
		t.Fatalf("Get on legacy record returned error: %v", err)
	}
	if !reflect.DeepEqual(gotVec, legacy) {
		t.Errorf("Get legacy vector = %v, want %v", gotVec, legacy)
	}
	if math.Abs(float64(gotNorm-5)) > 1e-6 {
		t.Errorf("Get legacy norm = %v, want 5", gotNorm)
	}
}

// writeRawGob stores arbitrary gob-encoded bytes under key id, bypassing the
// store's own encoding. Used to exercise the legacy and corrupt decode paths.
func writeRawGob(t *testing.T, s *BadgerStore, id string, v any) {
	t.Helper()
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(id), buf.Bytes())
	}); err != nil {
		t.Fatalf("failed to write raw record %s: %v", id, err)
	}
}

func TestGetVector_LegacyAndCorrupt(t *testing.T) {
	s := newTestStore(t)

	t.Run("legacy record migrates", func(t *testing.T) {
		writeRawGob(t, s, "legacy-getvector", []float32{6, 8}) // norm 10
		vec, err := s.GetVector("legacy-getvector")
		if err != nil {
			t.Fatalf("GetVector on legacy record returned error: %v", err)
		}
		if !reflect.DeepEqual(vec, []float32{6, 8}) {
			t.Errorf("GetVector legacy = %v, want {6 8}", vec)
		}
	})

	t.Run("corrupt record errors", func(t *testing.T) {
		if err := s.db.Update(func(txn *badger.Txn) error {
			return txn.Set([]byte("corrupt"), []byte("not gob data"))
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetVector("corrupt"); err == nil {
			t.Error("GetVector on corrupt record should return an error")
		}
	})
}

func TestGetAllVectors_Legacy(t *testing.T) {
	s := newTestStore(t)

	writeRawGob(t, s, "legacy-all", []float32{3, 4})
	if err := s.SaveVector("new-all", []float32{1, 0}); err != nil {
		t.Fatal(err)
	}

	all, err := s.GetAllVectors()
	if err != nil {
		t.Fatalf("GetAllVectors returned error: %v", err)
	}
	if !reflect.DeepEqual(all["legacy-all"], []float32{3, 4}) {
		t.Errorf("GetAllVectors legacy = %v, want {3 4}", all["legacy-all"])
	}
	if !reflect.DeepEqual(all["new-all"], []float32{1, 0}) {
		t.Errorf("GetAllVectors new = %v, want {1 0}", all["new-all"])
	}
}

func TestGetAllVectors_Corrupt(t *testing.T) {
	s := newTestStore(t)
	if err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("bad"), []byte("junk"))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAllVectors(); err == nil {
		t.Error("GetAllVectors on corrupt record should return an error")
	}
}

func TestSearch_EdgeCases(t *testing.T) {
	s := newTestStore(t)

	t.Run("empty store returns no results", func(t *testing.T) {
		results, err := s.Search([]float32{1, 0}, 5)
		if err != nil {
			t.Fatalf("Search on empty store returned error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Search on empty store returned %d results, want 0", len(results))
		}
	})

	t.Run("k zero returns all results", func(t *testing.T) {
		for id, vec := range map[string][]float32{"a": {1, 0}, "b": {1, 0}} {
			if err := s.Add(id, vec, nil); err != nil {
				t.Fatal(err)
			}
		}
		results, err := s.Search([]float32{1, 0}, 0)
		if err != nil {
			t.Fatalf("Search(k=0) returned error: %v", err)
		}
		if len(results) != 2 {
			t.Errorf("Search(k=0) returned %d results, want all 2", len(results))
		}
	})

	t.Run("zero-norm query and vectors skipped", func(t *testing.T) {
		// helper store: a nonzero vector + a zero vector.
		z := newTestStore(t)
		if err := z.Add("nonzero", []float32{1, 0}, nil); err != nil {
			t.Fatal(err)
		}
		if err := z.Add("zero-vec", []float32{0, 0}, nil); err != nil {
			t.Fatal(err)
		}
		// Zero query: the norm guard skips every candidate.
		res, err := z.Search([]float32{0, 0}, 10)
		if err != nil {
			t.Fatalf("Search(zero query) returned error: %v", err)
		}
		if len(res) != 0 {
			t.Errorf("Search(zero query) returned %d results, want 0", len(res))
		}
		// Nonzero query must skip the zero-vector row.
		res, err = z.Search([]float32{1, 0}, 10)
		if err != nil {
			t.Fatalf("Search(nonzero query) returned error: %v", err)
		}
		if len(res) != 1 || res[0].ID != "nonzero" {
			t.Errorf("Search(nonzero query) = %+v, want single 'nonzero'", res)
		}
	})

	t.Run("legacy record migrates and is searchable", func(t *testing.T) {
		l := newTestStore(t)
		writeRawGob(t, l, "legacy-search", []float32{1, 0})
		res, err := l.Search([]float32{1, 0}, 10)
		if err != nil {
			t.Fatalf("Search over legacy record returned error: %v", err)
		}
		if len(res) != 1 || res[0].ID != "legacy-search" {
			t.Errorf("Search over legacy = %+v, want single 'legacy-search'", res)
		}
	})

	t.Run("corrupt record errors the search", func(t *testing.T) {
		c := newTestStore(t)
		if err := c.db.Update(func(txn *badger.Txn) error {
			return txn.Set([]byte("corrupt"), []byte("junk"))
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Search([]float32{1, 0}, 10); err == nil {
			t.Error("Search over corrupt record should return an error")
		}
	})
}

func TestSavedAndImport_OnClosedStore(t *testing.T) {
	s := newTestStore(t)

	t.Run("SaveVector on closed store errors", func(t *testing.T) {
		_ = s.Close()
		if err := s.SaveVector("id", []float32{1, 0}); err == nil {
			t.Error("SaveVector on closed store should error")
		}
	})

	t.Run("Get on closed store errors", func(t *testing.T) {
		if _, _, _, err := s.Get("id"); err == nil {
			t.Error("Get on closed store should error")
		}
	})

	t.Run("ImportVectors on closed store errors", func(t *testing.T) {
		if err := s.ImportVectors(map[string][]float32{"x": {1, 0}}); err == nil {
			t.Error("ImportVectors on closed store should error")
		}
	})
}

func TestNewBadgerStore_InvalidPath(t *testing.T) {
	// A path that exists as a regular file cannot host a BadgerDB dir.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBadgerStore(file); err == nil {
		t.Error("NewBadgerStore on a file path should error")
	}
}

func TestDeleteStale(t *testing.T) {
	s := newTestStore(t)
	ids := []string{"a.go", "b.go#func", "c.go"}
	for i, id := range ids {
		if err := s.Add(id, []float32{float32(i), 0, 0}, nil); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}

	// Keep a.go and c.go, drop b.go#func.
	valid := map[string]struct{}{"a.go": {}, "c.go": {}}
	pruned, err := s.DeleteStale(valid)
	if err != nil {
		t.Fatalf("DeleteStale returned error: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	all, err := s.GetAllVectors()
	if err != nil {
		t.Fatalf("GetAllVectors returned error: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("vectors after prune = %d, want 2", len(all))
	}
	if _, ok := all["b.go#func"]; ok {
		t.Error("stale vector b.go#func survived pruning")
	}
	if _, _, _, err := s.Get("b.go#func"); err == nil {
		t.Error("Get on pruned id should error")
	}
}
