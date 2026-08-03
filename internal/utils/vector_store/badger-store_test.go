package vector_store

import (
	"bytes"
	"encoding/gob"
	"math"
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
