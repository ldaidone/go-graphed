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
	if link.SourceType != ir.LinkSourceInferred {
		t.Errorf("link SourceType = %q, want %q", link.SourceType, ir.LinkSourceInferred)
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
	if link.Weight != 0.6 {
		t.Errorf("link Weight = %v, want %v", link.Weight, 0.6)
	}
	if link.SourceType != ir.LinkSourceInferred {
		t.Errorf("link SourceType = %q, want %q", link.SourceType, ir.LinkSourceInferred)
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
	if graph.Links[0].SourceType != ir.LinkSourceInferred {
		t.Errorf("link SourceType = %q, want %q", graph.Links[0].SourceType, ir.LinkSourceInferred)
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
				{SourceID: "call.go#function:alpha", TargetID: "call.go#function:beta", Type: "calls", Weight: 1.0, SourceType: ir.LinkSourceExtracted},
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
	if link.SourceType != ir.LinkSourceExtracted {
		t.Errorf("link SourceType = %q, want %q (parser tags must survive the lift)", link.SourceType, ir.LinkSourceExtracted)
	}
}

func TestBuild_AllLinksTagged(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "store.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "store.go#Store", Type: "interface", Name: "Store"},
				{ID: "store.go#MemStore", Type: "struct", Name: "MemStore"},
			},
		},
		{
			Path:     "docs/Store.md",
			Format:   "markdown",
			Metadata: map[string]string{},
			Entities: []ir.Entity{},
			Links: []ir.Link{
				// Untagged: the analyzer must default it to "extracted".
				{SourceID: "store.go#Store", TargetID: "store.go#MemStore", Type: "references", Weight: 1.0},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	if len(graph.Links) == 0 {
		t.Fatal("expected links to be built")
	}

	// implements (inferred) + documents (inferred) + lifted untagged (extracted).
	for _, link := range graph.Links {
		if link.SourceType != ir.LinkSourceExtracted && link.SourceType != ir.LinkSourceInferred {
			t.Errorf("link %s -> %s has invalid SourceType %q", link.SourceID, link.TargetID, link.SourceType)
		}
	}
}

func TestBuild_PackageIndexGroupsFilesAndLinks(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "internal/ir/types.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "internal/ir/types.go#package", Type: "package", Name: "ir", Metadata: map[string]string{"package_path": "internal/ir"}},
				{ID: "internal/ir/types.go#Graph", Type: "struct", Name: "Graph"},
			},
		},
		{
			Path:     "internal/ir/helpers.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "internal/ir/helpers.go#package", Type: "package", Name: "ir", Metadata: map[string]string{"package_path": "internal/ir"}},
			},
		},
		{
			Path:     "internal/analyzer/analyzer.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "internal/analyzer/analyzer.go#package", Type: "package", Name: "analyzer", Metadata: map[string]string{"package_path": "internal/analyzer"}},
				{ID: "internal/analyzer/analyzer.go#import:github.com/ldaidone/go-graphed/internal/ir", Type: "import", Name: "github.com/ldaidone/go-graphed/internal/ir"},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	if len(graph.Packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(graph.Packages))
	}
	irPkg := graph.Packages["internal/ir"]
	if irPkg == nil {
		t.Fatal("expected package 'internal/ir' in index")
	}
	if irPkg.Name != "ir" {
		t.Errorf("package name = %q, want %q", irPkg.Name, "ir")
	}
	if len(irPkg.Files) != 2 {
		t.Errorf("expected 2 files in internal/ir package, got %d", len(irPkg.Files))
	}
	anPkg := graph.Packages["internal/analyzer"]
	if anPkg == nil {
		t.Fatal("expected package 'internal/analyzer' in index")
	}
	if len(anPkg.Imports) != 1 || anPkg.Imports[0] != "internal/ir" {
		t.Errorf("analyzer package Imports = %v, want [internal/ir]", anPkg.Imports)
	}

	// part_of links: one per file.
	partOf := 0
	imports := 0
	for _, link := range graph.Links {
		switch link.Type {
		case "part_of":
			partOf++
			if link.SourceType != ir.LinkSourceExtracted {
				t.Errorf("part_of SourceType = %q, want %q", link.SourceType, ir.LinkSourceExtracted)
			}
			if link.TargetID != "package:internal/ir" && link.TargetID != "package:internal/analyzer" {
				t.Errorf("unexpected part_of target %q", link.TargetID)
			}
		case "imports":
			imports++
			if link.SourceType != ir.LinkSourceInferred {
				t.Errorf("imports SourceType = %q, want %q", link.SourceType, ir.LinkSourceInferred)
			}
			if link.SourceID != "internal/analyzer/analyzer.go" || link.TargetID != "package:internal/ir" {
				t.Errorf("unexpected imports link %s -> %s", link.SourceID, link.TargetID)
			}
		}
	}
	if partOf != 3 {
		t.Errorf("expected 3 part_of links, got %d", partOf)
	}
	if imports != 1 {
		t.Errorf("expected 1 imports link, got %d", imports)
	}
}

func TestBuild_PackageIndexSkipsDocsWithoutPackageEntity(t *testing.T) {
	docs := []ir.Document{
		{
			Path:     "store.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "store.go#Store", Type: "struct", Name: "Store"},
			},
		},
	}
	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	if len(graph.Packages) != 0 {
		t.Errorf("expected no packages for hand-built fixture, got %d", len(graph.Packages))
	}
	for _, link := range graph.Links {
		if link.Type == "part_of" || link.Type == "imports" {
			t.Errorf("fixture without package entity produced %s link", link.Type)
		}
	}
}

func TestBuild_PackageIndexResolvesImportsWithAbsolutePaths(t *testing.T) {
	root := "/tmp/proj"
	docs := []ir.Document{
		{
			Path:     root + "/internal/ir/types.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: root + "/internal/ir/types.go#package", Type: "package", Name: "ir", Metadata: map[string]string{"package_path": root + "/internal/ir"}},
			},
		},
		{
			Path:     root + "/internal/analyzer/analyzer.go",
			Format:   "golang",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: root + "/internal/analyzer/analyzer.go#package", Type: "package", Name: "analyzer", Metadata: map[string]string{"package_path": root + "/internal/analyzer"}},
				{ID: root + "/internal/analyzer/analyzer.go#import:github.com/ldaidone/go-graphed/internal/ir", Type: "import", Name: "github.com/ldaidone/go-graphed/internal/ir"},
			},
		},
	}

	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}

	anPkg := graph.Packages[root+"/internal/analyzer"]
	if anPkg == nil {
		t.Fatal("expected package 'internal/analyzer' in index")
	}
	if len(anPkg.Imports) != 1 || anPkg.Imports[0] != root+"/internal/ir" {
		t.Errorf("analyzer package Imports = %v, want [%s]", anPkg.Imports, root+"/internal/ir")
	}
	irPkg := graph.Packages[root+"/internal/ir"]
	if irPkg == nil {
		t.Fatal("expected package 'internal/ir' in index")
	}
	if irPkg.ImportPath != "github.com/ldaidone/go-graphed/internal/ir" {
		t.Errorf("ir package ImportPath = %q, want %q", irPkg.ImportPath, "github.com/ldaidone/go-graphed/internal/ir")
	}

	imports := 0
	for _, link := range graph.Links {
		if link.Type == "imports" {
			imports++
			if link.SourceID != root+"/internal/analyzer/analyzer.go" || link.TargetID != "package:"+root+"/internal/ir" {
				t.Errorf("unexpected imports link %s -> %s", link.SourceID, link.TargetID)
			}
			if link.SourceType != ir.LinkSourceInferred {
				t.Errorf("imports SourceType = %q, want %q", link.SourceType, ir.LinkSourceInferred)
			}
		}
	}
	if imports != 1 {
		t.Errorf("expected 1 imports link, got %d", imports)
	}
}
func TestBuild_SetsBuiltAt(t *testing.T) {
	docs := []ir.Document{{Path: "a.go", Format: "golang", Metadata: map[string]string{}, Entities: []ir.Entity{}}}
	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	if graph.BuiltAt.IsZero() {
		t.Error("expected BuiltAt to be set by Build")
	}
}
