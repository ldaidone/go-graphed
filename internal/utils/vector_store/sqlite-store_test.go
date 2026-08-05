package vector_store

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ldaidone/goembedx/pkg/embedx"
)

// newTestSQLiteStore opens a SQLiteStore in a temp dir and registers cleanup.
func newTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteStore returned error: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSQLiteStore_ImplementsInterfaces(t *testing.T) {
	var _ Store = (*SQLiteStore)(nil)
	var _ embedx.VectorStore = (*SQLiteStore)(nil)
	var _ embedx.Store = (*SQLiteStore)(nil)
}

func TestSQLiteSaveAndGetVector(t *testing.T) {
	s := newTestSQLiteStore(t)
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

func TestSQLiteGetVector_NotFound(t *testing.T) {
	s := newTestSQLiteStore(t)
	if _, err := s.GetVector("missing"); err == nil {
		t.Error("GetVector on missing id should return an error")
	}
}

func TestSQLiteAddAndGetWithMeta(t *testing.T) {
	s := newTestSQLiteStore(t)
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

func TestSQLiteGet_NotFound(t *testing.T) {
	s := newTestSQLiteStore(t)
	if _, _, _, err := s.Get("missing"); err == nil {
		t.Error("Get on missing id should return an error")
	}
}

func TestSQLiteUpsertOverwrites(t *testing.T) {
	s := newTestSQLiteStore(t)
	if err := s.Add("id", []float32{1, 0, 0}, map[string]any{"v": 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("id", []float32{0, 1, 0}, map[string]any{"v": 2}); err != nil {
		t.Fatal(err)
	}

	vec, _, meta, err := s.Get("id")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(vec, []float32{0, 1, 0}) {
		t.Errorf("Get after overwrite = %v, want {0 1 0}", vec)
	}
	if meta["v"] != 2 {
		t.Errorf("meta after overwrite = %v, want v=2", meta)
	}
}

func TestSQLiteGetAllVectors(t *testing.T) {
	s := newTestSQLiteStore(t)
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

func TestSQLiteSearch_TopKOrdering(t *testing.T) {
	s := newTestSQLiteStore(t)
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

	top1, err := s.Search([]float32{1, 0, 0}, 1)
	if err != nil {
		t.Fatalf("Search(k=1) returned error: %v", err)
	}
	if len(top1) != 1 || top1[0].ID != "x" {
		t.Errorf("Search(k=1) = %+v, want single result x", top1)
	}
}

func TestSQLiteSearch_ExcludesDimensionMismatch(t *testing.T) {
	s := newTestSQLiteStore(t)
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

func TestSQLiteImportExportVectors(t *testing.T) {
	s := newTestSQLiteStore(t)
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

// writeRawBlob stores arbitrary gob-encoded bytes under key id, bypassing the
// store's own encoding. Used to exercise the legacy and corrupt decode paths.
func writeRawBlob(t *testing.T, s *SQLiteStore, id string, v any) {
	t.Helper()
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO vectors(id, data) VALUES(?, ?)`, id, buf.Bytes()); err != nil {
		t.Fatalf("failed to write raw record %s: %v", id, err)
	}
}

func TestSQLiteBackwardCompatibility(t *testing.T) {
	s := newTestSQLiteStore(t)

	legacy := []float32{3, 4} // norm should be 5
	writeRawBlob(t, s, "legacy-id", legacy)

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

func TestSQLiteGetVector_LegacyAndCorrupt(t *testing.T) {
	s := newTestSQLiteStore(t)

	t.Run("legacy record migrates", func(t *testing.T) {
		writeRawBlob(t, s, "legacy-getvector", []float32{6, 8}) // norm 10
		vec, err := s.GetVector("legacy-getvector")
		if err != nil {
			t.Fatalf("GetVector on legacy record returned error: %v", err)
		}
		if !reflect.DeepEqual(vec, []float32{6, 8}) {
			t.Errorf("GetVector legacy = %v, want {6 8}", vec)
		}
	})

	t.Run("corrupt record errors", func(t *testing.T) {
		if _, err := s.db.Exec(`INSERT INTO vectors(id, data) VALUES(?, ?)`, "corrupt", []byte("not gob data")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetVector("corrupt"); err == nil {
			t.Error("GetVector on corrupt record should return an error")
		}
	})
}

func TestSQLiteGetAllVectors_Legacy(t *testing.T) {
	s := newTestSQLiteStore(t)

	writeRawBlob(t, s, "legacy-all", []float32{3, 4})
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

func TestSQLiteGetAllVectors_Corrupt(t *testing.T) {
	s := newTestSQLiteStore(t)
	if _, err := s.db.Exec(`INSERT INTO vectors(id, data) VALUES(?, ?)`, "bad", []byte("junk")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAllVectors(); err == nil {
		t.Error("GetAllVectors on corrupt record should return an error")
	}
}

func TestSQLiteSearch_EdgeCases(t *testing.T) {
	s := newTestSQLiteStore(t)

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
		z := newTestSQLiteStore(t)
		if err := z.Add("nonzero", []float32{1, 0}, nil); err != nil {
			t.Fatal(err)
		}
		if err := z.Add("zero-vec", []float32{0, 0}, nil); err != nil {
			t.Fatal(err)
		}
		res, err := z.Search([]float32{0, 0}, 10)
		if err != nil {
			t.Fatalf("Search(zero query) returned error: %v", err)
		}
		if len(res) != 0 {
			t.Errorf("Search(zero query) returned %d results, want 0", len(res))
		}
		res, err = z.Search([]float32{1, 0}, 10)
		if err != nil {
			t.Fatalf("Search(nonzero query) returned error: %v", err)
		}
		if len(res) != 1 || res[0].ID != "nonzero" {
			t.Errorf("Search(nonzero query) = %+v, want single 'nonzero'", res)
		}
	})

	t.Run("legacy record migrates and is searchable", func(t *testing.T) {
		l := newTestSQLiteStore(t)
		writeRawBlob(t, l, "legacy-search", []float32{1, 0})
		res, err := l.Search([]float32{1, 0}, 10)
		if err != nil {
			t.Fatalf("Search over legacy record returned error: %v", err)
		}
		if len(res) != 1 || res[0].ID != "legacy-search" {
			t.Errorf("Search over legacy = %+v, want single 'legacy-search'", res)
		}
	})

	t.Run("corrupt record errors the search", func(t *testing.T) {
		c := newTestSQLiteStore(t)
		if _, err := c.db.Exec(`INSERT INTO vectors(id, data) VALUES(?, ?)`, "corrupt", []byte("junk")); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Search([]float32{1, 0}, 10); err == nil {
			t.Error("Search over corrupt record should return an error")
		}
	})
}

func TestSQLiteSavedAndImport_OnClosedStore(t *testing.T) {
	s := newTestSQLiteStore(t)

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

func TestSQLiteNew_InvalidDir(t *testing.T) {
	// A path that exists as a regular file cannot host a store directory.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSQLiteStore(file); err == nil {
		t.Error("NewSQLiteStore on a file path should error")
	}
}

func TestSQLiteNew_DirWithSpaces(t *testing.T) {
	// The config directory frequently contains spaces (e.g. a volume name);
	// the file: URI must escape them.
	dir := filepath.Join(t.TempDir(), "mac hd 1tb", "my project")
	s, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore on path with spaces returned error: %v", err)
	}
	defer s.Close()

	if err := s.Add("spaced", []float32{1, 0}, nil); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	vec, _, _, err := s.Get("spaced")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !reflect.DeepEqual(vec, []float32{1, 0}) {
		t.Errorf("Get = %v, want {1 0}", vec)
	}
}

func TestSQLiteDeleteStale(t *testing.T) {
	s := newTestSQLiteStore(t)
	ids := []string{"a.go", "b.go#func", "c.go"}
	for i, id := range ids {
		if err := s.Add(id, []float32{float32(i), 0, 0}, nil); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}

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

// TestSQLiteStore_MultipleHandlesSameDB simulates two MCP servers opening the
// same store directory: each NewSQLiteStore call is an independent connection
// pool to the same database file, so writes through one must be visible
// through the other and concurrent writes must not corrupt or fail.
func TestSQLiteStore_MultipleHandlesSameDB(t *testing.T) {
	dir := t.TempDir()

	a, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("first NewSQLiteStore returned error: %v", err)
	}
	defer a.Close()
	b, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("second NewSQLiteStore returned error: %v", err)
	}
	defer b.Close()

	if err := a.Add("from-a", []float32{1, 0, 0}, map[string]any{"writer": "a"}); err != nil {
		t.Fatalf("a.Add: %v", err)
	}
	vec, norm, meta, err := b.Get("from-a")
	if err != nil {
		t.Fatalf("b.Get: %v", err)
	}
	if len(vec) != 3 || vec[0] != 1 || norm != 1 || meta["writer"] != "a" {
		t.Errorf("b read via separate handle = vec %v, norm %v, meta %v", vec, norm, meta)
	}

	const each = 20
	var wg sync.WaitGroup
	for i := 0; i < each; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			if err := a.Add(fmt.Sprintf("a-%d", i), []float32{float32(i), 0, 0}, nil); err != nil {
				t.Errorf("a.Add(%d): %v", i, err)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			if err := b.Add(fmt.Sprintf("b-%d", i), []float32{0, float32(i), 0}, nil); err != nil {
				t.Errorf("b.Add(%d): %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	all, err := a.GetAllVectors()
	if err != nil {
		t.Fatalf("GetAllVectors: %v", err)
	}
	if len(all) != 1+each*2 {
		t.Errorf("vector count = %d, want %d", len(all), 1+each*2)
	}

	// Every stored b-* vector is a unit vector along the query axis, so they
	// all tie at score 1; the top result must be one of them, and must be a
	// record the second handle wrote.
	results, err := b.Search([]float32{0, 1, 0}, 5)
	if err != nil {
		t.Fatalf("b.Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("b.Search returned no results")
	}
	if got := results[0].ID; !strings.HasPrefix(got, "b-") {
		t.Errorf("top search result = %q, want a b-* vector", got)
	}
}

// TestSQLiteStore_MultipleProcesses spawns a second process that opens the
// same database file the parent already holds open, proving the store is safe
// across real processes (not just handles in one address space). This mirrors
// two MCP server processes sharing one store directory.
func TestSQLiteStore_MultipleProcesses(t *testing.T) {
	if os.Getenv("SQLITE_HELPER_PROCESS") == "1" {
		runSQLiteHelperProcess(t)
		return
	}

	dir := t.TempDir()
	a, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore returned error: %v", err)
	}
	defer a.Close()

	if err := a.Add("parent", []float32{1, 0, 0}, nil); err != nil {
		t.Fatalf("a.Add: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSQLiteStore_MultipleProcesses")
	cmd.Env = append(os.Environ(), "SQLITE_HELPER_PROCESS=1", "SQLITE_HELPER_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper process failed: %v\n%s", err, out)
	}

	// The parent handle must observe the child's write through its own pool.
	vec, _, _, err := a.Get("child")
	if err != nil {
		t.Fatalf("parent failed to read child's vector: %v", err)
	}
	if len(vec) != 3 || vec[0] != 0 || vec[1] != 1 {
		t.Errorf("child vector = %v, want {0 1 0}", vec)
	}
}

func runSQLiteHelperProcess(t *testing.T) {
	s, err := NewSQLiteStore(os.Getenv("SQLITE_HELPER_DIR"))
	if err != nil {
		t.Fatalf("child NewSQLiteStore: %v", err)
	}
	defer s.Close()

	if err := s.Add("child", []float32{0, 1, 0}, nil); err != nil {
		t.Fatalf("child Add: %v", err)
	}

	// The child must also read the record the parent wrote before spawning it.
	vec, _, _, err := s.Get("parent")
	if err != nil {
		t.Fatalf("child failed to read parent's vector: %v", err)
	}
	if len(vec) != 3 || vec[0] != 1 {
		t.Errorf("child saw parent vector = %v, want {1 0 0}", vec)
	}
}
