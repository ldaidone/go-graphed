package analyzer

import (
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
)

// doc is a shorthand for building a hand-built fixture document.
func importDoc(path, format string, imports []string) ir.Document {
	d := ir.Document{Path: path, Format: format, Metadata: map[string]string{}}
	for _, name := range imports {
		d.Entities = append(d.Entities, ir.Entity{
			ID:   path + "#import:" + name,
			Type: "import",
			Name: name,
		})
	}
	return d
}

func TestBuild_GenericImport_KotlinDottedPath(t *testing.T) {
	docs := []ir.Document{
		importDoc("controller/OrderController.kt", "kotlin", []string{"com.acme.orders.service.OrderService", "com.acme.orders.repository.OrderRepository"}),
		importDoc("service/OrderService.kt", "kotlin", nil),
		importDoc("repository/OrderRepository.kt", "kotlin", nil),
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	edges := map[string]bool{}
	for _, link := range graph.Links {
		if link.Type != "imports" {
			continue
		}
		if link.SourceType != ir.LinkSourceInferred || link.Weight != 0.8 {
			t.Errorf("imports edge %s -> %s SourceType=%q Weight=%v, want inferred 0.8", link.SourceID, link.TargetID, link.SourceType, link.Weight)
		}
		edges[link.SourceID+" -> "+link.TargetID] = true
	}
	for _, want := range []string{
		"controller/OrderController.kt -> service/OrderService.kt",
		"controller/OrderController.kt -> repository/OrderRepository.kt",
	} {
		if !edges[want] {
			t.Errorf("missing imports edge %q", want)
		}
	}
}

func TestBuild_GenericImport_KotlinNestedPackageSuffix(t *testing.T) {
	docs := []ir.Document{
		importDoc("src/main/kotlin/com/acme/orders/service/OrderService.kt", "kotlin", nil),
		importDoc("src/main/kotlin/com/acme/orders/controller/OrderController.kt", "kotlin", []string{"com.acme.orders.service.OrderService"}),
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	edges := map[string]bool{}
	for _, link := range graph.Links {
		if link.Type == "imports" {
			edges[link.SourceID+" -> "+link.TargetID] = true
		}
	}
	if !edges["src/main/kotlin/com/acme/orders/controller/OrderController.kt -> src/main/kotlin/com/acme/orders/service/OrderService.kt"] {
		t.Errorf("nested package import did not resolve via suffix matching: %v", edges)
	}
}

func TestBuild_GenericImport_CLocalHeaderExtracted(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "shapes.c",
			Format:   "c",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "shapes.c#import:shapes.h", Type: "import", Name: "shapes.h", Metadata: map[string]string{"include_kind": "local"}},
			},
		},
		{Path: "shapes.h", Format: "c", Metadata: map[string]string{}},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	if len(graph.Links) != 1 {
		t.Fatalf("expected 1 imports edge, got %d: %v", len(graph.Links), graph.Links)
	}
	link := graph.Links[0]
	if link.SourceID != "shapes.c" || link.TargetID != "shapes.h" {
		t.Errorf("unexpected imports edge %s -> %s", link.SourceID, link.TargetID)
	}
	if link.SourceType != ir.LinkSourceExtracted || link.Weight != 1.0 {
		t.Errorf("local header include SourceType=%q Weight=%v, want extracted 1.0", link.SourceType, link.Weight)
	}
}

func TestBuild_GenericImport_CSystemIncludeSkipped(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "main.c",
			Format:   "c",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "main.c#import:stdio.h", Type: "import", Name: "stdio.h", Metadata: map[string]string{"include_kind": "system"}},
			},
		},
	}
	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	for _, link := range graph.Links {
		if link.Type == "imports" {
			t.Errorf("system include produced an imports edge: %s -> %s", link.SourceID, link.TargetID)
		}
	}
}

func TestBuild_GenericImport_ElixirSnakeCaseModule(t *testing.T) {
	docs := []ir.Document{
		importDoc("lib/geometry/shapes.ex", "elixir", nil),
		importDoc("lib/app.ex", "elixir", []string{"Geometry.Shapes"}),
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	edges := map[string]bool{}
	for _, link := range graph.Links {
		if link.Type == "imports" {
			edges[link.SourceID+" -> "+link.TargetID] = true
		}
	}
	if !edges["lib/app.ex -> lib/geometry/shapes.ex"] {
		t.Errorf("Elixir snake_case module alias did not resolve: %v", edges)
	}
}

func TestBuild_GenericImport_PythonDottedModule(t *testing.T) {
	docs := []ir.Document{
		importDoc("service/release.py", "python", nil),
		importDoc("api/controller.py", "python", []string{"service.release"}),
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	edges := map[string]bool{}
	for _, link := range graph.Links {
		if link.Type == "imports" {
			edges[link.SourceID+" -> "+link.TargetID] = true
		}
	}
	if !edges["api/controller.py -> service/release.py"] {
		t.Errorf("Python dotted module import did not resolve: %v", edges)
	}
}

func TestBuild_GenericImport_RustSymbolDropped(t *testing.T) {
	docs := []ir.Document{
		importDoc("src/shapes.rs", "rust", nil),
		importDoc("src/main.rs", "rust", []string{"crate::shapes::Circle"}),
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	edges := map[string]bool{}
	for _, link := range graph.Links {
		if link.Type == "imports" {
			edges[link.SourceID+" -> "+link.TargetID] = true
		}
	}
	if !edges["src/main.rs -> src/shapes.rs"] {
		t.Errorf("Rust use-declaration with trailing symbol did not resolve: %v", edges)
	}
}

func TestBuild_GenericImport_ExternalBareNameSkipped(t *testing.T) {
	docs := []ir.Document{
		importDoc("src/App.kt", "kotlin", []string{"java.util.List", "org.springframework.stereotype.Service"}),
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	for _, link := range graph.Links {
		if link.Type == "imports" {
			t.Errorf("external bare import produced an imports edge: %s -> %s", link.SourceID, link.TargetID)
		}
	}
}

func TestBuild_GenericImport_PageRankAndHubsComeAlive(t *testing.T) {
	// A three-tier Kotlin project: controller -> service -> repo.
	docs := []ir.Document{
		importDoc("controller/OrderController.kt", "kotlin", []string{"com.acme.service.OrderService"}),
		importDoc("service/OrderService.kt", "kotlin", []string{"com.acme.repo.OrderRepository"}),
		importDoc("repo/OrderRepository.kt", "kotlin", nil),
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	if len(graph.Links) != 2 {
		t.Fatalf("expected 2 imports edges, got %d", len(graph.Links))
	}

	// The coupling graph is no longer empty, so PageRank must be non-zero
	// and at least one hub must be flagged.
	if graph.Metrics.HubCount == 0 {
		t.Error("expected at least one hub once cross-file imports exist")
	}
	nonzero := 0
	for path, dm := range graph.Metrics.Documents {
		if dm.PageRank > 0 {
			nonzero++
		}
		if dm.Degree == 0 {
			t.Errorf("document %s has degree 0 despite imports edges", path)
		}
	}
	if nonzero != 3 {
		t.Errorf("expected 3 documents with non-zero PageRank, got %d", nonzero)
	}
}
