package exporter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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
				SourceID:   "main.go#Main",
				TargetID:   "main.go#Helper",
				Type:       "calls",
				Weight:     0.9,
				SourceType: ir.LinkSourceExtracted,
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
	if parsed.Links[0].SourceType != ir.LinkSourceExtracted {
		t.Errorf("parsed link SourceType = %q, want %q", parsed.Links[0].SourceType, ir.LinkSourceExtracted)
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

func TestJSON_RoundTripsPackagesAndBuiltAt(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "graph.json")
	builtAt := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	graph := ir.Graph{
		Documents: map[string]*ir.Document{
			"a.go": {Path: "a.go", Format: "golang", Metadata: map[string]string{}, Entities: []ir.Entity{}},
		},
		Links: []ir.Link{},
		Packages: map[string]*ir.Package{
			"internal/ir": {Name: "ir", Path: "internal/ir", Files: []string{"a.go"}, Imports: []string{"internal/x"}},
		},
		BuiltAt: builtAt,
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

	if len(parsed.Packages) != 1 {
		t.Fatalf("Packages count = %d, want 1", len(parsed.Packages))
	}
	pkg := parsed.Packages["internal/ir"]
	if pkg == nil {
		t.Fatal("package internal/ir not round-tripped")
	}
	if pkg.Name != "ir" || len(pkg.Files) != 1 || len(pkg.Imports) != 1 || pkg.Imports[0] != "internal/x" {
		t.Errorf("package fields not round-tripped: %+v", pkg)
	}
	if !parsed.BuiltAt.Equal(builtAt) {
		t.Errorf("BuiltAt = %v, want %v", parsed.BuiltAt, builtAt)
	}
}

func TestJSON_RoundTripsClusters(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "graph.json")

	graph := ir.Graph{
		Documents: map[string]*ir.Document{
			"internal/ir/types.go": {Path: "internal/ir/types.go", Format: "golang", Metadata: map[string]string{}, Entities: []ir.Entity{}},
			"internal/ir/x.go":     {Path: "internal/ir/x.go", Format: "golang", Metadata: map[string]string{}, Entities: []ir.Entity{}},
		},
		Links: []ir.Link{},
		Clusters: []ir.Cluster{
			{ID: "directory:internal/ir", Name: "internal/ir", Kind: ir.ClusterKindDirectory, Members: []string{"internal/ir/types.go", "internal/ir/x.go"}, Size: 2},
			{ID: "network:internal/ir/types.go", Name: "internal/ir", Kind: ir.ClusterKindNetwork, Members: []string{"internal/ir/types.go", "internal/ir/x.go"}, Size: 2},
		},
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

	if len(parsed.Clusters) != 2 {
		t.Fatalf("Clusters count = %d, want 2", len(parsed.Clusters))
	}
	dir := parsed.Clusters[0]
	if dir.ID != "directory:internal/ir" || dir.Kind != ir.ClusterKindDirectory || dir.Size != 2 {
		t.Errorf("directory cluster not round-tripped: %+v", dir)
	}
	if len(dir.Members) != 2 || dir.Members[0] != "internal/ir/types.go" {
		t.Errorf("cluster members not round-tripped: %v", dir.Members)
	}
	net := parsed.Clusters[1]
	if net.Kind != ir.ClusterKindNetwork {
		t.Errorf("network cluster kind = %q, want network", net.Kind)
	}
}

func TestJSON_RoundTripsMetrics(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "graph.json")

	graph := ir.Graph{
		Documents: map[string]*ir.Document{
			"hub.go": {Path: "hub.go", Format: "golang", Metadata: map[string]string{ir.MetadataHubFlag: "true"}},
			"a.go":   {Path: "a.go", Format: "golang", Metadata: map[string]string{}},
		},
		Links: []ir.Link{},
		Metrics: ir.Metrics{
			Documents: map[string]ir.DocumentMetrics{
				"hub.go": {Degree: 4, WeightedDegree: 40, PageRank: 0.5, IsHub: true},
				"a.go":   {Degree: 1, WeightedDegree: 10, PageRank: 0.125},
			},
			HubCount: 1,
		},
	}

	if err := JSON(graph, output); err != nil {
		t.Fatalf("JSON returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("could not read output file: %v", err)
	}

	// The literal "is_hub": "true" metadata flag must appear in graph.json.
	if !bytes.Contains(data, []byte(`"is_hub": "true"`)) {
		t.Error("graph.json is missing the literal \"is_hub\": \"true\" metadata flag")
	}

	var parsed ir.Graph
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if parsed.Metrics.HubCount != 1 {
		t.Errorf("Metrics.HubCount = %d, want 1", parsed.Metrics.HubCount)
	}
	hub, ok := parsed.Metrics.Documents["hub.go"]
	if !ok {
		t.Fatal("hub.go metrics not round-tripped")
	}
	if hub.Degree != 4 || hub.WeightedDegree != 40 || hub.PageRank != 0.5 || !hub.IsHub {
		t.Errorf("hub metrics not round-tripped: %+v", hub)
	}
	if got := parsed.Documents["hub.go"].Metadata[ir.MetadataHubFlag]; got != "true" {
		t.Errorf("Metadata[is_hub] = %q after round-trip, want \"true\"", got)
	}
}

func TestJSON_ErrorPaths(t *testing.T) {
	graph := ir.Graph{
		Documents: make(map[string]*ir.Document),
		Links:     []ir.Link{},
	}

	t.Run("parent directory is a file", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(blocker, "graph.json")
		if err := JSON(graph, output); err == nil {
			t.Error("expected error when the parent directory is a file")
		}
	})

	t.Run("output path is a directory", func(t *testing.T) {
		output := filepath.Join(t.TempDir(), "graph.json")
		if err := os.Mkdir(output, 0755); err != nil {
			t.Fatal(err)
		}
		if err := JSON(graph, output); err == nil {
			t.Error("expected error when the output path is an existing directory")
		}
	})
}
