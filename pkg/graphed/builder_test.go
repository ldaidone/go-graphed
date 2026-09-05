package graphed_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
