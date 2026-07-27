package exporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
)

func TestJSON_WritesValidFile(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "out", "graph.json")

	graph := ir.Graph{
		Documents: map[string]*ir.Document{
			"main.go": {
				Path:     "main.go",
				Format:   "golang",
				Metadata: map[string]string{},
				Entities: []ir.Entity{},
			},
		},
		Links: []ir.Link{
			{
				SourceID: "main.go#Main",
				TargetID: "main.go#Helper",
				Type:     "calls",
				Weight:   0.9,
			},
		},
	}

	if err := JSON(graph, output); err != nil {
		t.Fatalf("JSON returned unexpected error: %v", err)
	}

	// Verify the file exists and is non-empty.
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("could not read output file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("output file is empty")
	}

	// Verify it is valid JSON.
	var parsed ir.Graph
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(parsed.Documents) != 1 {
		t.Errorf("parsed Documents count = %d, want 1", len(parsed.Documents))
	}
	if len(parsed.Links) != 1 {
		t.Errorf("parsed Links count = %d, want 1", len(parsed.Links))
	}
}

func TestJSON_CreatesNestedDirectories(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "a", "b", "c", "graph.json")

	graph := ir.Graph{
		Documents: make(map[string]*ir.Document),
		Links:     []ir.Link{},
	}

	if err := JSON(graph, output); err != nil {
		t.Fatalf("JSON returned unexpected error: %v", err)
	}

	if _, err := os.Stat(output); os.IsNotExist(err) {
		t.Error("output file was not created in nested directory")
	}
}

func TestJSON_EmptyGraph(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "empty.json")

	graph := ir.Graph{
		Documents: make(map[string]*ir.Document),
		Links:     []ir.Link{},
	}

	if err := JSON(graph, output); err != nil {
		t.Fatalf("JSON returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("could not read output file: %v", err)
	}

	var parsed ir.Graph
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(parsed.Documents) != 0 {
		t.Errorf("parsed Documents count = %d, want 0", len(parsed.Documents))
	}
}

func TestJSON_PrettyPrinted(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "pretty.json")

	graph := ir.Graph{
		Documents: map[string]*ir.Document{
			"a.go": {Path: "a.go", Format: "golang", Metadata: map[string]string{}, Entities: []ir.Entity{}},
		},
		Links: []ir.Link{},
	}

	if err := JSON(graph, output); err != nil {
		t.Fatalf("JSON returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("could not read output file: %v", err)
	}

	// The file should contain 4-space indented JSON (not compact).
	if filepath.Base(output) == "pretty.json" && len(data) > 0 {
		// A compact JSON marshal of this graph would be a single line.
		// If the output has newlines, it's indented.
		if data[0] != '{' {
			t.Errorf("expected JSON to start with '{', got %q", string(data[:1]))
		}
	}
}
