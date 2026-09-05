package analyzer

import (
	"math"
	"reflect"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
)

// starGraph builds a hand-written graph where hub.go links to four leaves.
// All links are entity-anchored calls with weight 1.0, so the document-level
// projection is a clean star: hub.go has degree 4, every leaf degree 1.
func starGraph() *ir.Graph {
	docs := map[string]*ir.Document{
		"hub.go": {Path: "hub.go", Metadata: map[string]string{}},
		"l1.go":  {Path: "l1.go", Metadata: map[string]string{}},
		"l2.go":  {Path: "l2.go", Metadata: map[string]string{}},
		"l3.go":  {Path: "l3.go", Metadata: map[string]string{}},
		"l4.go":  {Path: "l4.go", Metadata: map[string]string{}},
	}
	links := []ir.Link{
		{SourceID: "hub.go#Hub", TargetID: "l1.go#L1", Type: "calls", Weight: 1.0},
		{SourceID: "hub.go#Hub", TargetID: "l2.go#L2", Type: "calls", Weight: 1.0},
		{SourceID: "hub.go#Hub", TargetID: "l3.go#L3", Type: "calls", Weight: 1.0},
		{SourceID: "hub.go#Hub", TargetID: "l4.go#L4", Type: "calls", Weight: 1.0},
	}
	return &ir.Graph{Documents: docs, Links: links}
}

func TestAnnotateMetrics_StarGraphScoresAndFlagsHub(t *testing.T) {
	graph := starGraph()
	annotateMetrics(graph)

	if graph.Metrics.Documents == nil {
		t.Fatal("expected Metrics.Documents to be initialized")
	}
	if len(graph.Metrics.Documents) != 5 {
		t.Errorf("metrics count = %d, want 5 (every document gets a row)", len(graph.Metrics.Documents))
	}

	hub := graph.Metrics.Documents["hub.go"]
	if hub.Degree != 4 {
		t.Errorf("hub.go Degree = %d, want 4", hub.Degree)
	}
	if hub.WeightedDegree != 40 {
		t.Errorf("hub.go WeightedDegree = %v, want 40 (4 links scaled to integer tenths)", hub.WeightedDegree)
	}
	if !hub.IsHub {
		t.Error("hub.go should be flagged as a hub")
	}
	// The hub's PageRank must dominate every leaf's.
	for _, leaf := range []string{"l1.go", "l2.go", "l3.go", "l4.go"} {
		dm := graph.Metrics.Documents[leaf]
		if dm.Degree != 1 {
			t.Errorf("%s Degree = %d, want 1", leaf, dm.Degree)
		}
		if dm.IsHub {
			t.Errorf("%s should not be a hub (leaf)", leaf)
		}
		if dm.PageRank >= hub.PageRank {
			t.Errorf("%s PageRank %f should be below hub.go %f", leaf, dm.PageRank, hub.PageRank)
		}
	}

	if graph.Metrics.HubCount != 1 {
		t.Errorf("HubCount = %d, want 1", graph.Metrics.HubCount)
	}

	// The exported metadata flag must be present on the hub document only.
	if got := graph.Documents["hub.go"].Metadata[ir.MetadataHubFlag]; got != "true" {
		t.Errorf("hub.go Metadata[is_hub] = %q, want \"true\"", got)
	}
	for _, leaf := range []string{"l1.go", "l2.go", "l3.go", "l4.go"} {
		if _, ok := graph.Documents[leaf].Metadata[ir.MetadataHubFlag]; ok {
			t.Errorf("%s should not carry the is_hub flag", leaf)
		}
	}
}

func TestAnnotateMetrics_NilMetadataTolerated(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"hub.go": {Path: "hub.go"}, // Metadata nil on purpose
			"l1.go":  {Path: "l1.go"},
		},
		Links: []ir.Link{
			{SourceID: "hub.go#H", TargetID: "l1.go#L", Type: "calls", Weight: 1.0},
		},
	}
	annotateMetrics(graph) // must not panic
	if graph.Metrics.Documents["hub.go"].Degree != 1 {
		t.Errorf("hub.go Degree = %d, want 1", graph.Metrics.Documents["hub.go"].Degree)
	}
}

func TestAnnotateMetrics_IsolatedDocumentsStayZeroScored(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"a.go": {Path: "a.go", Metadata: map[string]string{}},
			"b.go": {Path: "b.go", Metadata: map[string]string{}},
		},
		Links: []ir.Link{},
	}
	annotateMetrics(graph)
	for _, path := range []string{"a.go", "b.go"} {
		dm := graph.Metrics.Documents[path]
		if dm.Degree != 0 || dm.WeightedDegree != 0 || dm.PageRank != 0 || dm.IsHub {
			t.Errorf("%s expected zero scores and no hub flag, got %+v", path, dm)
		}
	}
	if graph.Metrics.HubCount != 0 {
		t.Errorf("HubCount = %d, want 0 for an edgeless graph", graph.Metrics.HubCount)
	}
}

func TestAnnotateMetrics_MinDegreeRuleBlocksTinyPairs(t *testing.T) {
	graph := &ir.Graph{
		Documents: map[string]*ir.Document{
			"a.go": {Path: "a.go", Metadata: map[string]string{}},
			"b.go": {Path: "b.go", Metadata: map[string]string{}},
		},
		Links: []ir.Link{
			{SourceID: "a.go#A", TargetID: "b.go#B", Type: "calls", Weight: 1.0},
		},
	}
	annotateMetrics(graph)
	if graph.Metrics.HubCount != 0 {
		t.Errorf("HubCount = %d, want 0 (degree-1 documents are not God Nodes)", graph.Metrics.HubCount)
	}
}

func TestAnnotateMetrics_PageRankSumsToOne(t *testing.T) {
	graph := starGraph()
	annotateMetrics(graph)
	total := 0.0
	for _, dm := range graph.Metrics.Documents {
		if dm.Degree == 0 {
			continue // isolated docs contribute no mass
		}
		total += dm.PageRank
	}
	if math.Abs(total-1.0) > 1e-4 {
		t.Errorf("PageRank sums to %f, want ~1.0", total)
	}
}

func TestAnnotateMetrics_EmptyGraph(t *testing.T) {
	graph := &ir.Graph{Documents: map[string]*ir.Document{}, Links: []ir.Link{}}
	annotateMetrics(graph)
	if graph.Metrics.Documents == nil || len(graph.Metrics.Documents) != 0 {
		t.Errorf("expected empty metrics map, got %v", graph.Metrics.Documents)
	}
	if graph.Metrics.HubCount != 0 {
		t.Errorf("HubCount = %d, want 0", graph.Metrics.HubCount)
	}
}

func TestAnnotateMetrics_Deterministic(t *testing.T) {
	graph := starGraph()
	first := &ir.Graph{Documents: graph.Documents, Links: graph.Links}
	annotateMetrics(first)
	for range 10 {
		again := &ir.Graph{
			Documents: map[string]*ir.Document{
				"hub.go": {Path: "hub.go", Metadata: map[string]string{}},
				"l1.go":  {Path: "l1.go", Metadata: map[string]string{}},
				"l2.go":  {Path: "l2.go", Metadata: map[string]string{}},
				"l3.go":  {Path: "l3.go", Metadata: map[string]string{}},
				"l4.go":  {Path: "l4.go", Metadata: map[string]string{}},
			},
			Links: append([]ir.Link(nil), graph.Links...),
		}
		annotateMetrics(again)
		if !reflect.DeepEqual(first.Metrics, again.Metrics) {
			t.Fatalf("annotateMetrics is not deterministic:\nfirst: %+v\nagain: %+v", first.Metrics, again.Metrics)
		}
	}
}

func TestBuild_PopulatesMetricsAndHubFlag(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "hub.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "hub.go#Hub", Type: "struct", Name: "Hub"},
			},
		},
		{
			Path:     "l1.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "l1.go#L1", Type: "struct", Name: "L1"},
			},
		},
		{
			Path:     "l2.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "l2.go#L2", Type: "struct", Name: "L2"},
			},
		},
	}
	// Simulate parser-emitted call links between hub and leaves.
	docs[0].Links = []ir.Link{
		{SourceID: "hub.go#Hub", TargetID: "l1.go#L1", Type: "calls", Weight: 1.0, SourceType: ir.LinkSourceExtracted},
		{SourceID: "hub.go#Hub", TargetID: "l2.go#L2", Type: "calls", Weight: 1.0, SourceType: ir.LinkSourceExtracted},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	if len(graph.Metrics.Documents) != 3 {
		t.Fatalf("metrics count = %d, want 3", len(graph.Metrics.Documents))
	}
	if hub := graph.Metrics.Documents["hub.go"]; !hub.IsHub {
		t.Errorf("hub.go should be flagged a hub via Build, got %+v", hub)
	}
	if got := graph.Documents["hub.go"].Metadata[ir.MetadataHubFlag]; got != "true" {
		t.Errorf("hub.go Metadata[is_hub] = %q, want \"true\" in the built graph", got)
	}
	if graph.Metrics.HubCount != 1 {
		t.Errorf("HubCount = %d, want 1", graph.Metrics.HubCount)
	}
}

func TestBuild_DocumentKeywordLinksDoNotSkewHubMetrics(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "hub.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{{ID: "hub.go#Hub", Type: "struct", Name: "Hub"}},
		},
		{
			Path:     "l1.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{{ID: "l1.go#L1", Type: "struct", Name: "L1"}},
		},
		{
			Path:     "l2.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{{ID: "l2.go#L2", Type: "struct", Name: "L2"}},
		},
		{
			// Path contains every entity name, so the analyzer's
			// "documents" keyword pass fires three 0.6-weight edges.
			Path:     "docs/hub_l1_l2.md",
			Format:   "markdown",
			Metadata: map[string]string{},
			Entities: []ir.Entity{},
		},
	}
	// hub.go couples to both leaves via real extracted edges.
	docs[0].Links = []ir.Link{
		{SourceID: "hub.go#Hub", TargetID: "l1.go#L1", Type: "calls", Weight: 1.0, SourceType: ir.LinkSourceExtracted},
		{SourceID: "hub.go#Hub", TargetID: "l2.go#L2", Type: "calls", Weight: 1.0, SourceType: ir.LinkSourceExtracted},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	// The markdown file must stay decoupled from hub ranking: its keyword
	// links are excluded from the coupling graph.
	md := graph.Metrics.Documents["docs/hub_l1_l2.md"]
	if md.Degree != 0 {
		t.Errorf("markdown doc Degree = %d, want 0 (keyword links must not count)", md.Degree)
	}
	if md.IsHub {
		t.Error("markdown doc should not be flagged a hub from keyword links alone")
	}
	if hub := graph.Metrics.Documents["hub.go"]; !hub.IsHub {
		t.Errorf("hub.go should be the hub, got %+v", hub)
	}
	// The inferred keyword edges still exist in the graph for consumers
	// that want doc-to-code associations.
	found := false
	for _, link := range graph.Links {
		if link.Type == "documents" && link.SourceID == "docs/hub_l1_l2.md" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a documents keyword link to remain in graph.Links")
	}
}
