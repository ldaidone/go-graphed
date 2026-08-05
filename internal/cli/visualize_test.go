package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/exporter"
	"github.com/ldaidone/go-graphed/internal/ir"
)

func TestVisualizeCmd_FlagDefaults(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"visualize"})
	if err != nil || cmd == nil {
		t.Fatal("visualize command not found")
	}
	file, err := cmd.Flags().GetString("file")
	if err != nil {
		t.Fatalf("file flag: %v", err)
	}
	if file != "graph.json" {
		t.Errorf("file default = %q, want %q", file, "graph.json")
	}
	html, err := cmd.Flags().GetString("html")
	if err != nil {
		t.Fatalf("html flag: %v", err)
	}
	if html != "graph.html" {
		t.Errorf("html default = %q, want %q", html, "graph.html")
	}
	report, err := cmd.Flags().GetString("report")
	if err != nil {
		t.Fatalf("report flag: %v", err)
	}
	if report != "GRAPH_REPORT.md" {
		t.Errorf("report default = %q, want %q", report, "GRAPH_REPORT.md")
	}
}

func TestVisualizeCmd_EndToEnd(t *testing.T) {
	tmp := t.TempDir()
	graphPath := filepath.Join(tmp, "graph.json")
	htmlPath := filepath.Join(tmp, "out", "graph.html")
	mdPath := filepath.Join(tmp, "out", "GRAPH_REPORT.md")

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
	if err := exporter.JSON(graph, graphPath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	prev := RootCmd.SilenceUsage
	RootCmd.SilenceUsage = true
	defer func() { RootCmd.SilenceUsage = prev }()

	RootCmd.SetArgs([]string{"visualize", "-f", graphPath, "--html", htmlPath, "--report", mdPath})
	defer RootCmd.SetArgs(nil)

	if err := Execute(); err != nil {
		t.Fatalf("visualize command failed: %v", err)
	}

	for path, want := range map[string]string{
		htmlPath: "Knowledge Graph Visualizer",
		mdPath:   "# Knowledge Graph Report",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("output %s not written: %v", path, err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s missing %q", path, want)
		}
	}
}
