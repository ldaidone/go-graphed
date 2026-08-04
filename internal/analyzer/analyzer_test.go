package analyzer

import (
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
)

func TestBuild_EmptyInput(t *testing.T) {
	graph, err := Build(nil)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	if len(graph.Documents) != 0 {
		t.Errorf("expected 0 documents, got %d", len(graph.Documents))
	}
	if len(graph.Links) != 0 {
		t.Errorf("expected 0 links, got %d", len(graph.Links))
	}
}

func TestBuild_DocumentsIndexedByPath(t *testing.T) {
	docs := []ir.Document{
		{Path: "a.go", Format: "golang", Metadata: map[string]string{}, Entities: []ir.Entity{}},
		{Path: "b.go", Format: "golang", Metadata: map[string]string{}, Entities: []ir.Entity{}},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	if len(graph.Documents) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(graph.Documents))
	}
	if _, ok := graph.Documents["a.go"]; !ok {
		t.Error("document a.go not found in graph")
	}
	if _, ok := graph.Documents["b.go"]; !ok {
		t.Error("document b.go not found in graph")
	}
}

func TestBuild_ImplementsLinkByNamingConvention(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "store.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{
					ID:   "store.go#Store",
					Type: "interface",
					Name: "Store",
				},
			},
		},
		{
			Path:     "memstore.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{
					ID:   "memstore.go#MemStore",
					Type: "struct",
					Name: "MemStore",
				},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	// MemStore has suffix "Store" which matches the interface name.
	if len(graph.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(graph.Links))
	}

	link := graph.Links[0]
	if link.Type != "implements" {
		t.Errorf("link Type = %q, want %q", link.Type, "implements")
	}
	if link.SourceID != "memstore.go#MemStore" {
		t.Errorf("link SourceID = %q, want %q", link.SourceID, "memstore.go#MemStore")
	}
	if link.TargetID != "store.go#Store" {
		t.Errorf("link TargetID = %q, want %q", link.TargetID, "store.go#Store")
	}
	if link.Weight != 0.8 {
		t.Errorf("link Weight = %v, want %v", link.Weight, 0.8)
	}
	if link.Metadata["rule"] != "naming_convention_heuristic" {
		t.Errorf("link rule = %q, want %q", link.Metadata["rule"], "naming_convention_heuristic")
	}
}

func TestBuild_NoLinkWhenSuffixMismatch(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "reader.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{
					ID:   "reader.go#Reader",
					Type: "interface",
					Name: "Reader",
				},
			},
		},
		{
			Path:     "writer.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{
					ID:   "writer.go#Writer",
					Type: "struct",
					Name: "Writer",
				},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	if len(graph.Links) != 0 {
		t.Errorf("expected 0 links, got %d", len(graph.Links))
	}
}

func TestBuild_DocumentsLinkFromMarkdown(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "docs/Store.md",
			Format:   "markdown",
			Metadata: map[string]string{},
			Entities: []ir.Entity{},
		},
		{
			Path:     "store.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{
					ID:   "store.go#Store",
					Type: "struct",
					Name: "Store",
				},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	// The markdown path "docs/Store.md" contains "Store" (case-insensitive).
	if len(graph.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(graph.Links))
	}

	link := graph.Links[0]
	if link.Type != "documents" {
		t.Errorf("link Type = %q, want %q", link.Type, "documents")
	}
	if link.SourceID != "docs/Store.md" {
		t.Errorf("link SourceID = %q, want %q", link.SourceID, "docs/Store.md")
	}
	if link.TargetID != "store.go#Store" {
		t.Errorf("link TargetID = %q, want %q", link.TargetID, "store.go#Store")
	}
	if link.Weight != 1.0 {
		t.Errorf("link Weight = %v, want %v", link.Weight, 1.0)
	}
}

func TestBuild_PdfAlsoTriggersDocumentsLink(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "docs/Config.pdf",
			Format:   "pdf",
			Metadata: map[string]string{},
			Entities: []ir.Entity{},
		},
		{
			Path:     "config.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{
					ID:   "config.go#Config",
					Type: "struct",
					Name: "Config",
				},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	if len(graph.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(graph.Links))
	}
	if graph.Links[0].Type != "documents" {
		t.Errorf("link Type = %q, want %q", graph.Links[0].Type, "documents")
	}
}

func TestBuild_GoFileDoesNotTriggerDocumentsLink(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "Store.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{},
		},
		{
			Path:     "store.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{
					ID:   "store.go#Store",
					Type: "struct",
					Name: "Store",
				},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	// Go files should NOT produce "documents" links even if the path matches.
	if len(graph.Links) != 0 {
		t.Errorf("expected 0 links for Go files, got %d", len(graph.Links))
	}
}

func TestBuild_MultipleImplementsLinks(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "io.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "io.go#Reader", Type: "interface", Name: "Reader"},
				{ID: "io.go#Writer", Type: "interface", Name: "Writer"},
			},
		},
		{
			Path:     "impls.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				// "MyReader" ends with "Reader", "MyWriter" ends with "Writer".
				{ID: "impls.go#MyReader", Type: "struct", Name: "MyReader"},
				{ID: "impls.go#MyWriter", Type: "struct", Name: "MyWriter"},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	// MyReader -> Reader, MyWriter -> Writer: two implements links.
	if len(graph.Links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(graph.Links))
	}

	for _, link := range graph.Links {
		if link.Type != "implements" {
			t.Errorf("link Type = %q, want %q", link.Type, "implements")
		}
	}
}

func TestBuild_LiftsDocumentLinks(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "call.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "call.go#function:alpha", Type: "function", Name: "alpha"},
				{ID: "call.go#function:beta", Type: "function", Name: "beta"},
			},
			Links: []ir.Link{
				{SourceID: "call.go#function:alpha", TargetID: "call.go#function:beta", Type: "calls", Weight: 1.0},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	if len(graph.Links) != 1 {
		t.Fatalf("expected 1 lifted link, got %d", len(graph.Links))
	}
	link := graph.Links[0]
	if link.Type != "calls" {
		t.Errorf("link Type = %q, want calls", link.Type)
	}
	if link.SourceID != "call.go#function:alpha" || link.TargetID != "call.go#function:beta" {
		t.Errorf("unexpected link endpoints: %s -> %s", link.SourceID, link.TargetID)
	}
}
