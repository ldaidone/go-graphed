package analyzer

import (
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
)

func TestBuild_GenericPackageAggregationAndModuleClusters(t *testing.T) {
	// Two Kotlin files declaring the same package and one declaring a
	// different one must aggregate into two package nodes, emit part_of
	// edges, and produce module clusters like Go packages do.
	docs := []ir.Document{
		{
			Path:     "controller/OrderController.kt",
			Format:   "kotlin",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "controller/OrderController.kt#package", Type: "package", Name: "com.acme.orders", Metadata: map[string]string{"package_path": "com.acme.orders"}},
			},
		},
		{
			Path:     "service/OrderService.kt",
			Format:   "kotlin",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "service/OrderService.kt#package", Type: "package", Name: "com.acme.orders", Metadata: map[string]string{"package_path": "com.acme.orders"}},
			},
		},
		{
			Path:     "test/AcceptanceTest.kt",
			Format:   "kotlin",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "test/AcceptanceTest.kt#package", Type: "package", Name: "com.acme.orders.test", Metadata: map[string]string{"package_path": "com.acme.orders.test"}},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	if len(graph.Packages) != 2 {
		t.Fatalf("expected 2 packages, got %d: %v", len(graph.Packages), graph.Packages)
	}
	orders := graph.Packages["com.acme.orders"]
	if orders == nil || len(orders.Files) != 2 {
		t.Errorf("com.acme.orders package files = %v, want both controller and service", graph.Packages["com.acme.orders"])
	}

	partOf := 0
	for _, link := range graph.Links {
		if link.Type == "part_of" {
			partOf++
			if link.SourceType != ir.LinkSourceExtracted || link.Weight != 1.0 {
				t.Errorf("part_of edge SourceType=%q Weight=%v, want extracted 1.0", link.SourceType, link.Weight)
			}
			if link.TargetID != ir.PackageNodeID("com.acme.orders") &&
				link.TargetID != ir.PackageNodeID("com.acme.orders.test") {
				t.Errorf("part_of target %q is not a package node", link.TargetID)
			}
		}
	}
	if partOf != 3 {
		t.Errorf("expected 3 part_of edges, got %d", partOf)
	}

	moduleClusters := 0
	for _, c := range graph.Clusters {
		if c.Kind == ir.ClusterKindModule {
			moduleClusters++
		}
	}
	if moduleClusters != 2 {
		t.Errorf("expected 2 module clusters, got %d", moduleClusters)
	}

	// The package node must expand to its member files in the coupling
	// graph, so metrics see non-zero degree for members even without any
	// import edges.
	if graph.Metrics.Documents["controller/OrderController.kt"].Degree == 0 {
		t.Error("expected controller to couple to its sibling via part_of edges")
	}
}

func TestBuild_PackageNodeExpandsToFilesInCoupling(t *testing.T) {
	// A single package with two files: the part_of edges must couple the
	// members together (package node expands to both files), giving each
	// a non-zero degree.
	docs := []ir.Document{
		{
			Path:     "lib/a.swift",
			Format:   "swift",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "lib/a.swift#package", Type: "package", Name: "MyLib", Metadata: map[string]string{"package_path": "MyLib"}},
			},
		},
		{
			Path:     "lib/b.swift",
			Format:   "swift",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "lib/b.swift#package", Type: "package", Name: "MyLib", Metadata: map[string]string{"package_path": "MyLib"}},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	for _, p := range []string{"lib/a.swift", "lib/b.swift"} {
		if graph.Metrics.Documents[p].Degree == 0 {
			t.Errorf("expected %s to have non-zero coupling degree via package node", p)
		}
	}
}
