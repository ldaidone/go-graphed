package graphed_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/pkg/graphed"
)

func TestBadgerDBPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := config.BadgerDBPath("")
	if err != nil {
		t.Fatalf("BadgerDBPath returned error: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "graphed", filepath.Base(cwd))
	if got != want {
		t.Errorf("BadgerDBPath() = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("expected db directory to exist: %v", err)
	}
}

func TestBuild_ProducesGraphJSON(t *testing.T) {
	tmp := t.TempDir()
	src := `package store

type Store interface {
	Get(key string) (any, error)
}

type MemStore struct {
	data map[string]any
}
`
	path := filepath.Join(tmp, "store.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "out", "graph.json")

	opts := graphed.BuildOptions{
		Root:   tmp,
		Output: output,
		Format: "json",
	}
	if err := graphed.Build(opts); err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}

	var graph ir.Graph
	if err := json.Unmarshal(data, &graph); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(graph.Documents) != 1 {
		t.Fatalf("documents = %d, want 1", len(graph.Documents))
	}
	doc, ok := graph.Documents[path]
	if !ok {
		t.Fatalf("document %q not found in graph", path)
	}
	if doc.Format != "golang" {
		t.Errorf("Format = %q, want %q", doc.Format, "golang")
	}
	// package clause + Store interface + MemStore struct.
	if len(doc.Entities) != 3 {
		t.Errorf("entities = %d, want 3 (package + interface + struct)", len(doc.Entities))
	}

	// Cross-file package indexing should register the package and timestamp
	// the snapshot so freshness can be reported later.
	if len(graph.Packages) != 1 || graph.Packages[tmp] == nil {
		t.Errorf("expected package index entry for %q, got %v", tmp, graph.Packages)
	}
	if graph.BuiltAt.IsZero() {
		t.Error("expected BuiltAt to be set on the exported graph")
	}

	// MemStore ends with Store -> the implements naming heuristic should fire.
	if len(graph.Links) == 0 {
		t.Error("expected at least one link (implements naming heuristic)")
	}
}

func TestBuild_NonexistentRoot(t *testing.T) {
	opts := graphed.BuildOptions{
		Root:   "/nonexistent/root/that/does/not/exist",
		Output: filepath.Join(t.TempDir(), "graph.json"),
		Format: "json",
	}
	if err := graphed.Build(opts); err == nil {
		t.Error("Build on nonexistent root should return an error")
	}
}

func TestBuild_InvalidModelPathReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package a\n\ntype X struct{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	opts := graphed.BuildOptions{
		Root:      tmp,
		Output:    filepath.Join(t.TempDir(), "graph.json"),
		Format:    "json",
		ModelPath: "/nonexistent/model.gtemodel",
	}
	if err := graphed.Build(opts); err == nil {
		t.Error("Build with invalid model path should return an error")
	}
}

func TestBuild_WithConcurrentJobs(t *testing.T) {
	tmp := t.TempDir()
	for i := range 8 {
		content := "package p" + string(rune('a'+i)) + "\n\ntype T" + string(rune('A'+i)) + " struct{}\n"
		if err := os.WriteFile(filepath.Join(tmp, "file_"+string(rune('a'+i))+".go"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	output := filepath.Join(t.TempDir(), "out", "graph.json")
	opts := graphed.BuildOptions{
		Root:   tmp,
		Output: output,
		Format: "json",
		Jobs:   4,
	}
	if err := graphed.Build(opts); err != nil {
		t.Fatalf("Build with Jobs=4 returned error: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	var graph ir.Graph
	if err := json.Unmarshal(data, &graph); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(graph.Documents) != 8 {
		t.Errorf("documents = %d, want 8", len(graph.Documents))
	}
}

func TestBuild_DimensionsMismatchErrors(t *testing.T) {
	// The cross-check needs a real model; skip when it is not present so the
	// suite still runs in minimal checkouts.
	modelPath := filepath.Join("..", "..", config.ModelFileName)
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("model not found at %s", modelPath)
	}
	t.Setenv("HOME", t.TempDir())

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package a\n\ntype X struct{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	opts := graphed.BuildOptions{
		Root:       tmp,
		Output:     filepath.Join(t.TempDir(), "graph.json"),
		Format:     "json",
		ModelPath:  modelPath,
		Dimensions: 999,
	}
	err := graphed.Build(opts)
	if err == nil {
		t.Fatal("Build with mismatched Dimensions should return an error")
	}
	if !strings.Contains(err.Error(), "dimension mismatch") {
		t.Errorf("error = %q, want it to mention the dimension mismatch", err)
	}
}

func initGitRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := wt.Commit("init", &gogit.CommitOptions{Author: &object.Signature{Name: "T", Email: "t@x", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}
}

func TestBuild_GitAware_StampsMetadataAndCoChange(t *testing.T) {
	tmp := t.TempDir()
	initGitRepo(t, tmp, map[string]string{
		"a.go": "package a\n\ntype A struct{}\n",
		"b.go": "package b\n\ntype B struct{}\n",
	})
	output := filepath.Join(t.TempDir(), "graph.json")
	opts := graphed.BuildOptions{
		Root:     tmp,
		Output:   output,
		Format:   "json",
		GitAware: true,
		GitLimit: 10,
	}
	if err := graphed.Build(opts); err != nil {
		t.Fatalf("GitAware Build returned error: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var graph ir.Graph
	if err := json.Unmarshal(data, &graph); err != nil {
		t.Fatal(err)
	}
	if len(graph.Documents) != 2 {
		t.Fatalf("documents = %d, want 2", len(graph.Documents))
	}
	for path, doc := range graph.Documents {
		if doc.Metadata["git_status"] == "" {
			t.Errorf("%s missing git_status metadata", path)
		}
		if doc.Metadata["git_head"] == "" {
			t.Errorf("%s missing git_head metadata", path)
		}
	}
	found := false
	for _, l := range graph.Links {
		if l.Type == "co_changed" {
			found = true
			if l.SourceType != ir.LinkSourceInferred {
				t.Errorf("co_changed SourceType = %q, want inferred", l.SourceType)
			}
		}
	}
	if !found {
		t.Error("expected at least one co_changed link from the shared init commit")
	}
}

func TestBuild_ChangedOnly_ScopesRescanButKeepsSnapshot(t *testing.T) {
	// Non-first run: the filter must still scope the rescan (an on-disk
	// edit to a status-clean file is NOT picked up) while the snapshot
	// keeps the old entry for it.
	tmp := t.TempDir()
	initGitRepo(t, tmp, map[string]string{
		"a.go": "package a\n\ntype A struct{}\n",
		"b.go": "package b\n\ntype B struct{}\n",
	})
	output := filepath.Join(t.TempDir(), "graph.json")
	full := graphed.BuildOptions{Root: tmp, Output: output, Format: "json"}
	if err := graphed.Build(full); err != nil {
		t.Fatal(err)
	}

	// a.go: uncommitted edit (status M) -> rescanned.
	if err := os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package a\n\ntype A struct{}\n\ntype A2 struct{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// b.go: edit committed (status clean) -> must NOT be rescanned, but the
	// old snapshot entry must survive.
	if err := os.WriteFile(filepath.Join(tmp, "b.go"), []byte("package b\n\ntype B struct{}\n\ntype B2 struct{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	repo, err := gogit.PlainOpen(tmp)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("b.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("b2", &gogit.CommitOptions{Author: &object.Signature{Name: "T", Email: "t@x", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}

	changed := graphed.BuildOptions{Root: tmp, Output: output, Format: "json", ChangedOnly: true}
	if err := graphed.Build(changed); err != nil {
		t.Fatalf("ChangedOnly Build returned error: %v", err)
	}
	graph := loadGraph(t, output)
	if len(graph.Documents) != 2 {
		t.Fatalf("documents = %d, want 2 (snapshot preserved)", len(graph.Documents))
	}
	hasEntity := func(path, name string) bool {
		doc, ok := graph.Documents[path]
		if !ok {
			return false
		}
		for _, e := range doc.Entities {
			if e.Name == name {
				return true
			}
		}
		return false
	}
	if !hasEntity(filepath.Join(tmp, "a.go"), "A2") {
		t.Error("modified a.go was not rescanned (A2 missing)")
	}
	if hasEntity(filepath.Join(tmp, "b.go"), "B2") {
		t.Error("status-clean b.go must not be rescanned (B2 present)")
	}
	if _, ok := graph.Documents[filepath.Join(tmp, "b.go")]; !ok {
		t.Error("status-clean b.go dropped from snapshot")
	}
}

func TestBuild_GitAware_NonRepoDegradesGracefully(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package a\n\ntype X struct{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "graph.json")
	opts := graphed.BuildOptions{
		Root:     tmp,
		Output:   output,
		Format:   "json",
		GitAware: true,
	}
	if err := graphed.Build(opts); err != nil {
		t.Fatalf("GitAware on non-repo should degrade gracefully, got: %v", err)
	}
}

func loadGraph(t *testing.T, path string) ir.Graph {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var graph ir.Graph
	if err := json.Unmarshal(data, &graph); err != nil {
		t.Fatal(err)
	}
	return graph
}

func TestBuild_ChangedOnlyPatchesExistingSnapshot(t *testing.T) {
	// The init-then-changed-only scenario: a full snapshot exists, a
	// changed-only rebuild must patch it, not shrink it to changed files.
	tmp := t.TempDir()
	initGitRepo(t, tmp, map[string]string{
		"a.go": "package a\n\ntype A struct{}\n",
		"b.go": "package b\n\ntype B struct{}\n",
	})
	output := filepath.Join(t.TempDir(), "graph.json")
	full := graphed.BuildOptions{Root: tmp, Output: output, Format: "json"}
	if err := graphed.Build(full); err != nil {
		t.Fatal(err)
	}
	if got := loadGraph(t, output); len(got.Documents) != 2 {
		t.Fatalf("full build documents = %d, want 2", len(got.Documents))
	}

	// Modify only a.go (adding a struct), then rebuild changed-only.
	aPath := filepath.Join(tmp, "a.go")
	if err := os.WriteFile(aPath, []byte("package a\n\ntype A struct{}\n\ntype A2 struct{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	changed := graphed.BuildOptions{Root: tmp, Output: output, Format: "json", ChangedOnly: true}
	if err := graphed.Build(changed); err != nil {
		t.Fatal(err)
	}
	graph := loadGraph(t, output)
	if len(graph.Documents) != 2 {
		t.Fatalf("patched documents = %d, want 2 (b.go must survive)", len(graph.Documents))
	}
	aDoc, ok := graph.Documents[aPath]
	if !ok {
		t.Fatalf("modified a.go missing after patch, got %v", graph.Documents)
	}
	found := false
	for _, e := range aDoc.Entities {
		if e.Name == "A2" {
			found = true
		}
	}
	if !found {
		t.Errorf("a.go entities %v do not include the fresh A2 struct", aDoc.Entities)
	}
	if _, ok := graph.Documents[filepath.Join(tmp, "b.go")]; !ok {
		t.Error("unchanged b.go dropped by changed-only rebuild")
	}
}

func TestBuild_ChangedOnlyFirstRunScansFully(t *testing.T) {
	// Fresh clone scenario: clean tree (nothing changed), no snapshot yet.
	// --changed-only must fall back to a full scan, not write an empty graph.
	tmp := t.TempDir()
	initGitRepo(t, tmp, map[string]string{
		"a.go": "package a\n\ntype A struct{}\n",
		"b.go": "package b\n\ntype B struct{}\n",
	})
	output := filepath.Join(t.TempDir(), "graph.json")
	opts := graphed.BuildOptions{Root: tmp, Output: output, Format: "json", ChangedOnly: true}
	if err := graphed.Build(opts); err != nil {
		t.Fatal(err)
	}
	if got := loadGraph(t, output); len(got.Documents) != 2 {
		t.Errorf("first-run changed-only documents = %d, want 2 (full scan)", len(got.Documents))
	}
}

func TestBuild_DropsDeletedFiles(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package a\n\ntype A struct{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "b.go"), []byte("package b\n\ntype B struct{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "graph.json")
	opts := graphed.BuildOptions{Root: tmp, Output: output, Format: "json"}
	if err := graphed.Build(opts); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(tmp, "b.go")); err != nil {
		t.Fatal(err)
	}
	if err := graphed.Build(opts); err != nil {
		t.Fatal(err)
	}
	if got := loadGraph(t, output); len(got.Documents) != 1 {
		t.Errorf("documents after delete = %d, want 1", len(got.Documents))
	}
}

func TestBuild_CorruptSnapshotBuildsFresh(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package a\n\ntype A struct{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(output, []byte(`{"documents": `), 0644); err != nil {
		t.Fatal(err)
	}
	opts := graphed.BuildOptions{Root: tmp, Output: output, Format: "json"}
	if err := graphed.Build(opts); err != nil {
		t.Fatalf("corrupt snapshot should degrade to a fresh build, got: %v", err)
	}
	if got := loadGraph(t, output); len(got.Documents) != 1 {
		t.Errorf("documents = %d, want 1", len(got.Documents))
	}
}
