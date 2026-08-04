package exporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ldaidone/go-graphed/internal/ir"
)

// writeGraphFile writes a raw JSON payload to a temp file and returns its path.
func writeGraphFile(t *testing.T, payload string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(path, []byte(payload), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadGraph_EmptyPath(t *testing.T) {
	_, err := LoadGraph("")
	if err != ErrEmptyGraphFilePath {
		t.Errorf("LoadGraph(\"\") error = %v, want %v", err, ErrEmptyGraphFilePath)
	}
}

func TestLoadGraph_NonexistentFile(t *testing.T) {
	_, err := LoadGraph("/nonexistent/graph.json")
	if err == nil {
		t.Error("LoadGraph on nonexistent file should return an error")
	}
}

func TestLoadGraph_ValidGraph(t *testing.T) {
	builtAt := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	graph := ir.Graph{
		Documents: map[string]*ir.Document{
			"a.go": {
				Path:     "a.go",
				Format:   "golang",
				Size:     12,
				Metadata: map[string]string{"processor": "x"},
				Entities: []ir.Entity{
					{ID: "a.go#Foo", Type: "struct", Name: "Foo", Metadata: map[string]string{"start_line": "1"}},
				},
			},
		},
		Links: []ir.Link{
			{SourceID: "a.go", TargetID: "a.go#Foo", Type: "contains", Weight: 1.0},
		},
		Packages: map[string]*ir.Package{
			"pkg": {Name: "pkg", Path: "pkg", Files: []string{"a.go"}},
		},
		Clusters: []ir.Cluster{
			{ID: "directory:internal", Name: "internal", Kind: ir.ClusterKindDirectory, Members: []string{"a.go"}, Size: 1},
		},
		BuiltAt: builtAt,
	}

	data, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	path := writeGraphFile(t, string(data))

	loaded, err := LoadGraph(path)
	if err != nil {
		t.Fatalf("LoadGraph returned unexpected error: %v", err)
	}

	if len(loaded.Documents) != 1 {
		t.Errorf("documents count = %d, want 1", len(loaded.Documents))
	}
	doc, ok := loaded.Documents["a.go"]
	if !ok {
		t.Fatal("document a.go not loaded")
	}
	if doc.Format != "golang" {
		t.Errorf("Format = %q, want %q", doc.Format, "golang")
	}
	if len(doc.Entities) != 1 {
		t.Errorf("entity count = %d, want 1", len(doc.Entities))
	}
	if len(loaded.Links) != 1 {
		t.Errorf("link count = %d, want 1", len(loaded.Links))
	}
	if len(loaded.Packages) != 1 || loaded.Packages["pkg"] == nil {
		t.Errorf("packages not loaded: %v", loaded.Packages)
	}
	if len(loaded.Clusters) != 1 || loaded.Clusters[0].ID != "directory:internal" {
		t.Errorf("clusters not loaded: %v", loaded.Clusters)
	}
	if !loaded.BuiltAt.Equal(builtAt) {
		t.Errorf("BuiltAt = %v, want %v", loaded.BuiltAt, builtAt)
	}
	// Legacy graphs predate SourceType: the field loads as the zero value
	// (no default injected at load time); consumers normalize at display.
	if loaded.Links[0].SourceType != "" {
		t.Errorf("link SourceType = %q, want empty for legacy graph", loaded.Links[0].SourceType)
	}
}

func TestLoadGraph_InvalidJSON(t *testing.T) {
	path := writeGraphFile(t, `{"documents": `)
	_, err := LoadGraph(path)
	if err == nil {
		t.Error("LoadGraph on invalid JSON should return an error")
	}
}

func TestLoadGraph_MissingDocuments(t *testing.T) {
	path := writeGraphFile(t, `{"links": []}`)
	_, err := LoadGraph(path)
	if err == nil {
		t.Fatal("LoadGraph without documents should return an error")
	}
	if !strings.Contains(err.Error(), ErrCorruptGraphSchema.Error()) {
		t.Errorf("error = %v, want wrapped %v", err, ErrCorruptGraphSchema)
	}
}

func TestLoadGraph_MissingLinks(t *testing.T) {
	path := writeGraphFile(t, `{"documents": {"a.go": {"Path": "a.go", "Format": "golang", "Metadata": {}, "Entities": []}}}`)
	_, err := LoadGraph(path)
	if err == nil {
		t.Fatal("LoadGraph without links should return an error")
	}
	if !strings.Contains(err.Error(), ErrCorruptGraphSchema.Error()) {
		t.Errorf("error = %v, want wrapped %v", err, ErrCorruptGraphSchema)
	}
}

func TestLoadGraph_EmptyDocuments(t *testing.T) {
	path := writeGraphFile(t, `{"documents": {}, "links": []}`)
	_, err := LoadGraph(path)
	if err == nil {
		t.Fatal("LoadGraph with zero documents should return an error")
	}
	if !strings.Contains(err.Error(), ErrCorruptGraphSchema.Error()) {
		t.Errorf("error = %v, want wrapped %v", err, ErrCorruptGraphSchema)
	}
}

func TestLoadGraph_RejectsUnknownFields(t *testing.T) {
	path := writeGraphFile(t, `{"documents": {"a.go": {"Path": "a.go", "Format": "golang", "Metadata": {}, "Entities": []}}, "links": [], "bogus_field": true}`)
	_, err := LoadGraph(path)
	if err == nil {
		t.Error("LoadGraph with unknown top-level field should fail (DisallowUnknownFields)")
	}
}

func TestValidateGraphStructure_NilGraph(t *testing.T) {
	if err := validateGraphStructure(nil); err == nil {
		t.Error("validateGraphStructure(nil) should return an error")
	}
}
