package exporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
)

// visualizeFixture returns a small graph exercising the document projection:
// entity anchors collapse onto files, package nodes expand to member files,
// parallel edges collapse onto the heaviest one, below-threshold edges are
// dropped, and edges to unindexed files (ghost.c) are filtered out.
func visualizeFixture() ir.Graph {
	return ir.Graph{
		Documents: map[string]*ir.Document{
			"hub.go": {Path: "hub.go", Format: "golang", Metadata: map[string]string{}},
			"a.go":   {Path: "a.go", Format: "golang", Metadata: map[string]string{}},
			"b.md":   {Path: "b.md", Format: "markdown", Metadata: map[string]string{}},
		},
		Packages: map[string]*ir.Package{
			"internal/core": {Name: "core", Path: "internal/core", Files: []string{"hub.go", "a.go"}},
		},
		Metrics: ir.Metrics{
			Documents: map[string]ir.DocumentMetrics{
				"hub.go": {Degree: 4, WeightedDegree: 40, PageRank: 0.5, IsHub: true},
				"a.go":   {Degree: 1, WeightedDegree: 10, PageRank: 0.125},
				"b.md":   {},
			},
			HubCount: 1,
		},
		Links: []ir.Link{
			{SourceID: "hub.go#Hub", TargetID: "a.go#A", Type: "calls", Weight: 0.9, SourceType: ir.LinkSourceExtracted},
			{SourceID: "hub.go", TargetID: "a.go", Type: "references", Weight: 0.6, SourceType: ir.LinkSourceInferred},
			{SourceID: ir.PackageNodeID("internal/core"), TargetID: "b.md#Heading", Type: "mentions", Weight: 0.4, SourceType: ir.LinkSourceInferred},
			{SourceID: "hub.go#Hub", TargetID: "ghost.c#G", Type: "calls", Weight: 0.8, SourceType: ir.LinkSourceExtracted},
		},
	}
}

func TestBuildHTMLData_ProjectsDocumentGraph(t *testing.T) {
	graph := visualizeFixture()
	data := buildHTMLData(&graph)

	if len(data.Nodes) != 3 {
		t.Fatalf("node count = %d, want 3 (unindexed ghost.c dropped)", len(data.Nodes))
	}
	if data.Stats.Documents != 3 || data.Stats.Hubs != 1 {
		t.Errorf("stats = %+v, want 3 docs / 1 hub", data.Stats)
	}

	// Nodes sort by path.
	wantOrder := []string{"a.go", "b.md", "hub.go"}
	for i, want := range wantOrder {
		if data.Nodes[i].ID != want {
			t.Errorf("node[%d] = %q, want %q", i, data.Nodes[i].ID, want)
		}
	}

	// Hub flag and scores come through.
	hub := data.Nodes[2]
	if !hub.IsHub || hub.Degree != 4 || hub.Weighted != 40 || hub.PageRank != 0.5 {
		t.Errorf("hub node = %+v, want degree 4 / weighted 40 / pagerank 0.5 / isHub", hub)
	}

	// Package node expands onto member files; the strongest parallel edge
	// (0.9 extracted) wins; the below-threshold package->b.md edge is dropped.
	if len(data.Links) != 1 {
		t.Fatalf("link count = %d, want 1 aggregated edge", len(data.Links))
	}
	link := data.Links[0]
	if data.Nodes[link.Source].ID != "hub.go" || data.Nodes[link.Target].ID != "a.go" {
		t.Errorf("link endpoints = %s -> %s, want hub.go -> a.go", data.Nodes[link.Source].ID, data.Nodes[link.Target].ID)
	}
	if link.Weight != 0.9 {
		t.Errorf("link weight = %v, want 0.9 (heaviest parallel edge wins)", link.Weight)
	}
	if link.SourceType != ir.LinkSourceExtracted {
		t.Errorf("link sourceType = %q, want %q", link.SourceType, ir.LinkSourceExtracted)
	}
}

func TestBuildHTMLData_HubFlagFallsBackToMetadata(t *testing.T) {
	graph := ir.Graph{
		Documents: map[string]*ir.Document{
			"hub.go": {Path: "hub.go", Format: "golang", Metadata: map[string]string{ir.MetadataHubFlag: "true"}},
			"a.go":   {Path: "a.go", Format: "golang", Metadata: map[string]string{}},
		},
		Links: []ir.Link{},
	}
	data := buildHTMLData(&graph)
	if data.Stats.Hubs != 1 {
		t.Fatalf("hub count = %d, want 1 (from metadata fallback)", data.Stats.Hubs)
	}
	if !data.Nodes[1].IsHub {
		t.Errorf("hub.go not flagged as hub via metadata fallback: %+v", data.Nodes)
	}
}

func TestBuildHTMLData_CommonRoot(t *testing.T) {
	graph := ir.Graph{
		Documents: map[string]*ir.Document{
			"pkg/graphed/builder.go": {Path: "pkg/graphed/builder.go", Format: "golang"},
			"pkg/graphed/scanner.go": {Path: "pkg/graphed/scanner.go", Format: "golang"},
		},
		Links: []ir.Link{},
	}
	data := buildHTMLData(&graph)
	if data.Stats.Root != "pkg/graphed" {
		t.Errorf("root = %q, want %q", data.Stats.Root, "pkg/graphed")
	}
}

// extractVisualizerPayload pulls the inline data payload back out of the
// rendered HTML so the test can verify it is valid JSON.
func extractVisualizerPayload(t *testing.T, html string) []byte {
	t.Helper()
	marker := "var data = "
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatal("visualizer payload marker not found")
	}
	start := strings.Index(html[i:], "{")
	if start < 0 {
		t.Fatal("visualizer payload object not found")
	}
	start += i
	depth := 0
	for j := start; j < len(html); j++ {
		switch html[j] {
		case '{':
			depth++
		case '}':
			depth--
		}
		if depth == 0 {
			return []byte(html[start : j+1])
		}
	}
	t.Fatal("visualizer payload object not closed")
	return nil
}

func TestHTML_WritesSelfContainedFile(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "out", "graph.html")

	if err := HTML(visualizeFixture(), output); err != nil {
		t.Fatalf("HTML returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("could not read output file: %v", err)
	}
	html := string(data)

	// Self-contained: no external asset references.
	for _, forbid := range []string{"http://", "https://", "<script src", "<link"} {
		if strings.Contains(html, forbid) {
			t.Errorf("self-contained HTML must not reference external assets, found %q", forbid)
		}
	}

	// Placeholder must be replaced.
	if strings.Contains(html, htmlDataPlaceholder) {
		t.Error("data placeholder was not substituted")
	}

	// Document paths appear in the payload.
	payload := extractVisualizerPayload(t, html)
	var parsed htmlData
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("inline payload is not valid JSON: %v", err)
	}
	if parsed.Stats.Documents != 3 || parsed.Stats.Hubs != 1 {
		t.Errorf("parsed stats = %+v, want 3 docs / 1 hub", parsed.Stats)
	}
	found := false
	for _, n := range parsed.Nodes {
		if n.ID == "hub.go" && n.IsHub {
			found = true
		}
	}
	if !found {
		t.Error("payload missing hub.go hub node")
	}
}

func TestHTML_EmptyGraph(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "graph.html")

	graph := ir.Graph{Documents: map[string]*ir.Document{}, Links: []ir.Link{}}
	if err := HTML(graph, output); err != nil {
		t.Fatalf("HTML returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("could not read output file: %v", err)
	}
	if !strings.Contains(string(data), "No documents in this graph snapshot") {
		t.Error("empty graph should render the empty-state message")
	}
}
