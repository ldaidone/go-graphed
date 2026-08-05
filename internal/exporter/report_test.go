package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ldaidone/go-graphed/internal/ir"
)

func reportFixture() ir.Graph {
	return ir.Graph{
		BuiltAt: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
		Documents: map[string]*ir.Document{
			"pkg/hub.go":     {Path: "pkg/hub.go", Format: "golang", Metadata: map[string]string{}},
			"pkg/leaf.go":    {Path: "pkg/leaf.go", Format: "golang", Metadata: map[string]string{}},
			"docs/readme.md": {Path: "docs/readme.md", Format: "markdown", Metadata: map[string]string{}},
		},
		Links: []ir.Link{},
		Metrics: ir.Metrics{
			Documents: map[string]ir.DocumentMetrics{
				"pkg/hub.go":  {Degree: 4, WeightedDegree: 40, PageRank: 0.5, IsHub: true},
				"pkg/leaf.go": {Degree: 1, WeightedDegree: 10, PageRank: 0.125},
			},
			HubCount: 1,
		},
		Clusters: []ir.Cluster{
			{ID: "directory:pkg", Name: "pkg", Kind: ir.ClusterKindDirectory, Members: []string{"pkg/hub.go", "pkg/leaf.go"}, Size: 2},
			{ID: "network:docs/readme.md", Name: "docs_group", Kind: ir.ClusterKindNetwork, Members: []string{"docs/readme.md"}, Size: 1},
		},
	}
}

func TestBuildMarkdownReport_ContainsKeySections(t *testing.T) {
	graph := reportFixture()
	text := buildMarkdownReport(&graph)

	for _, want := range []string{
		"# Knowledge Graph Report",
		"**Generated:** 2026-08-04T12:00:00Z",
		"**Documents:** 3",
		"**Links:** 0",
		"**Hub (God Node) documents:** 1",
		"**Formats:** golang (2), markdown (1)",
		"## Hub Documents (God Nodes)",
		"| `pkg/hub.go` | 4 | 40.00 | 0.50000 |",
		"## Most Coupled Documents",
		"| `pkg/leaf.go` | 1 | 10.00 |",
		"## Clusters",
		"### directory",
		"### network",
		"`docs_group`",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report missing %q:\n%s", want, text)
		}
	}

	// Hub ranks above the non-hub in the most-coupled table.
	if strings.Index(text, "pkg/hub.go") > strings.Index(text, "pkg/leaf.go") {
		t.Errorf("hub should rank above leaf by weighted degree:\n%s", text)
	}
}

func TestBuildMarkdownReport_NoMetricsAndNoClusters(t *testing.T) {
	graph := ir.Graph{
		Documents: map[string]*ir.Document{
			"a.go": {Path: "a.go", Format: "golang", Metadata: map[string]string{ir.MetadataHubFlag: "true"}},
		},
		Links: []ir.Link{},
	}
	text := buildMarkdownReport(&graph)

	// Hub still surfaces via the metadata fallback.
	if !strings.Contains(text, "| `a.go` |") {
		t.Errorf("hub should surface from metadata fallback:\n%s", text)
	}
	if !strings.Contains(text, "_No clusters in this snapshot") {
		t.Errorf("missing no-clusters note:\n%s", text)
	}
}

func TestReport_WritesNestedDirectory(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "a", "b", "GRAPH_REPORT.md")

	if err := Report(reportFixture(), output); err != nil {
		t.Fatalf("Report returned unexpected error: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("could not read output file: %v", err)
	}
	if !strings.Contains(string(data), "# Knowledge Graph Report") {
		t.Error("report file missing header")
	}
}
