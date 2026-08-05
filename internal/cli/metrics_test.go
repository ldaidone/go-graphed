package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
)

func TestRenderMetricsSummary_HubsFirst(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"hub.go":      {Path: "hub.go", Format: "golang"},
			"leaf.go":     {Path: "leaf.go", Format: "golang"},
			"isolated.md": {Path: "isolated.md", Format: "markdown"},
		},
		Metrics: ir.Metrics{
			Documents: map[string]ir.DocumentMetrics{
				"hub.go":      {Degree: 4, WeightedDegree: 40, PageRank: 0.5, IsHub: true},
				"leaf.go":     {Degree: 1, WeightedDegree: 10, PageRank: 0.125},
				"isolated.md": {},
			},
			HubCount: 1,
		},
	}

	var buf bytes.Buffer
	if err := renderMetricsSummary(&buf, graph); err != nil {
		t.Fatalf("renderMetricsSummary returned error: %v", err)
	}
	text := buf.String()

	for _, want := range []string{
		"Documents indexed: 3",
		"Hub (God Node) count: 1",
		"[HUB]",
		"hub.go",
		"leaf.go",
		"isolated.md",
		"degree=4",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report missing %q:\n%s", want, text)
		}
	}

	hubPos := strings.Index(text, "[HUB]")
	leafPos := strings.Index(text, "leaf.go")
	if hubPos < 0 || leafPos < 0 || hubPos > leafPos {
		t.Errorf("hub should be listed before non-hub docs (hub=%d leaf=%d):\n%s", hubPos, leafPos, text)
	}
}

func TestRenderMetricsSummary_NoMetrics(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{"a.go": {Path: "a.go"}},
		Links:     []ir.Link{},
	}
	var buf bytes.Buffer
	if err := renderMetricsSummary(&buf, graph); err != nil {
		t.Fatalf("renderMetricsSummary returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "No centrality metrics") {
		t.Errorf("expected no-metrics message, got: %s", buf.String())
	}
}

func TestRenderMetricsSummary_ZeroScoredDocuments(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{"a.go": {Path: "a.go"}},
		Metrics:   ir.Metrics{Documents: map[string]ir.DocumentMetrics{"a.go": {}}},
	}
	var buf bytes.Buffer
	if err := renderMetricsSummary(&buf, graph); err != nil {
		t.Fatalf("renderMetricsSummary returned error: %v", err)
	}
	text := buf.String()
	if !strings.Contains(text, "a.go") {
		t.Errorf("expected zero-scored document to be listed, got: %s", text)
	}
	if !strings.Contains(text, "degree=0") {
		t.Errorf("expected zero degree in report, got: %s", text)
	}
}
