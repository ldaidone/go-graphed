package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/goembedx/pkg/embedx"
	mcp_golang "github.com/metoro-io/mcp-golang"
)

// responseText extracts the concatenated text content from a tool response.
func responseText(t *testing.T, resp *mcp_golang.ToolResponse) string {
	t.Helper()
	if resp == nil {
		t.Fatal("response is nil")
	}
	var sb strings.Builder
	for _, c := range resp.Content {
		if c.TextContent != nil {
			sb.WriteString(c.TextContent.Text)
		}
	}
	return sb.String()
}

// testServer builds a Server with an in-memory graph fixture, no transport needed
// since the handlers only touch s.graph.
func testServer() *Server {
	docA := &ir.Document{
		Path:      "internal/store/store.go",
		Format:    "golang",
		Size:      2048,
		UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Metadata:  map[string]string{"processor": "tree-sitter"},
		Entities: []ir.Entity{
			{ID: "internal/store/store.go#Store", Type: "interface", Name: "Store", Metadata: map[string]string{"start_line": "3"}},
		},
	}
	docB := &ir.Document{
		Path:     "README.md",
		Format:   "markdown",
		Size:     512,
		Metadata: map[string]string{},
		Entities: []ir.Entity{},
	}
	docC := &ir.Document{
		Path:     "internal/store/memstore.go",
		Format:   "golang",
		Size:     1024,
		Metadata: map[string]string{},
		Entities: []ir.Entity{
			{ID: "internal/store/memstore.go#MemStore", Type: "struct", Name: "MemStore"},
		},
	}

	return &Server{graph: &ir.Graph{
		Documents: map[string]*ir.Document{
			docA.Path: docA,
			docB.Path: docB,
			docC.Path: docC,
		},
		Links: []ir.Link{
			{SourceID: "internal/store/memstore.go#MemStore", TargetID: "internal/store/store.go#Store", Type: "implements", Weight: 0.8, SourceType: ir.LinkSourceInferred},
			{SourceID: "README.md", TargetID: "internal/store/store.go#Store", Type: "documents", Weight: 0.6, SourceType: ir.LinkSourceInferred},
		},
		Clusters: []ir.Cluster{
			{ID: "directory:internal/store", Name: "internal/store", Kind: ir.ClusterKindDirectory, Members: []string{"internal/store/memstore.go", "internal/store/store.go"}, Size: 2},
			{ID: "directory:.", Name: ".", Kind: ir.ClusterKindDirectory, Members: []string{"README.md"}, Size: 1},
			{ID: "network:internal/store/store.go", Name: "internal/store", Kind: ir.ClusterKindNetwork, Members: []string{"README.md", "internal/store/memstore.go", "internal/store/store.go"}, Size: 3},
		},
	}}
}

func TestHandleGetDocumentDetails_Found(t *testing.T) {
	s := testServer()
	resp, err := s.handleGetDocumentDetails(DocumentQueryArgs{Path: "internal/store/store.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	for _, want := range []string{
		"Document Details: internal/store/store.go",
		"golang",
		"2048 bytes",
		"tree-sitter",
		"Store",
		"interface",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("response missing %q:\n%s", want, text)
		}
	}
}

func TestHandleGetDocumentDetails_NotFound(t *testing.T) {
	s := testServer()
	resp, err := s.handleGetDocumentDetails(DocumentQueryArgs{Path: "does/not/exist.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), "not found") {
		t.Errorf("expected 'not found' message, got: %s", responseText(t, resp))
	}
}

func TestHandleGetDocumentDetails_NilGraph(t *testing.T) {
	s := &Server{graph: nil}
	_, err := s.handleGetDocumentDetails(DocumentQueryArgs{Path: "x.go"})
	if err == nil {
		t.Error("expected error for nil graph")
	}
}

func TestHandleGetDocumentLinks_OutgoingAndIncoming(t *testing.T) {
	s := testServer()
	resp, err := s.handleGetDocumentLinks(DocumentQueryArgs{Path: "internal/store/store.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	// store.go is the target of both links (incoming), and has no outgoing links.
	if !strings.Contains(text, "No outgoing references discovered") {
		t.Errorf("expected no outgoing references, got:\n%s", text)
	}
	if !strings.Contains(text, "implements") || !strings.Contains(text, "documents") {
		t.Errorf("expected incoming implements+documents links, got:\n%s", text)
	}
	if !strings.Contains(text, "inferred") {
		t.Errorf("expected provenance tag 'inferred' in link output, got:\n%s", text)
	}
}

func TestHandleGetDocumentLinks_EntityAnchoredSource(t *testing.T) {
	s := testServer()
	resp, err := s.handleGetDocumentLinks(DocumentQueryArgs{Path: "internal/store/memstore.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	// The implements link's SourceID is entity-anchored (memstore.go#MemStore);
	// it must still be counted as outgoing for the parent path.
	if !strings.Contains(text, "implements") {
		t.Errorf("expected outgoing implements link, got:\n%s", text)
	}
}

func TestHandleGetDocumentLinks_NotFound(t *testing.T) {
	s := testServer()
	resp, err := s.handleGetDocumentLinks(DocumentQueryArgs{Path: "missing.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), "does not exist") {
		t.Errorf("expected 'does not exist' message, got: %s", responseText(t, resp))
	}
}

func TestHandleListDocumentsByFormat_CaseInsensitive(t *testing.T) {
	s := testServer()
	resp, err := s.handleListDocumentsByFormat(FilterQueryArgs{Format: "GoLang"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	for _, want := range []string{"store.go", "memstore.go"} {
		if !strings.Contains(text, want) {
			t.Errorf("response missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "README.md") {
		t.Errorf("response should not contain README.md:\n%s", text)
	}
}

func TestHandleListDocumentsByFormat_NoMatch(t *testing.T) {
	s := testServer()
	resp, err := s.handleListDocumentsByFormat(FilterQueryArgs{Format: "rust"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), "No registered documents match") {
		t.Errorf("expected no-match message, got: %s", responseText(t, resp))
	}
}

func TestHandleFindEntitiesByType(t *testing.T) {
	s := testServer()
	resp, err := s.handleFindEntitiesByType(EntityFilterArgs{Type: "interface"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	if !strings.Contains(text, "Store") {
		t.Errorf("expected Store entity in response:\n%s", text)
	}
	if strings.Contains(text, "MemStore") {
		t.Errorf("MemStore is a struct, should not match interface filter:\n%s", text)
	}
}

func TestHandleFindEntitiesByType_NoMatch(t *testing.T) {
	s := testServer()
	resp, err := s.handleFindEntitiesByType(EntityFilterArgs{Type: "person"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), "No extracted elements found") {
		t.Errorf("expected no-match message, got: %s", responseText(t, resp))
	}
}

// fakeEmbedder implements TextEmbedder without loading a real model.
type fakeEmbedder struct{}

func (f *fakeEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func (f *fakeEmbedder) Close(ctx context.Context) error { return nil }

func TestHandleGetNarrowedContext_UninitializedIndex(t *testing.T) {
	s := &Server{graph: &ir.Graph{
		Documents: map[string]*ir.Document{},
		Links:     []ir.Link{},
	}}
	_, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   "a.go",
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
	})
	if err == nil || !strings.Contains(err.Error(), "uninitialized") {
		t.Errorf("expected 'uninitialized' error, got: %v", err)
	}
}

func TestHandleGetNarrowedContext_NilGraph(t *testing.T) {
	s := &Server{graph: nil}
	_, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   "a.go",
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
	})
	if err == nil || !strings.Contains(err.Error(), "uninitialized") {
		t.Errorf("expected 'uninitialized' error, got: %v", err)
	}
}

func TestHandleGetNarrowedContext_HappyPath(t *testing.T) {
	const entry = "internal/parser/go-extractor.go"
	const neighbor = "internal/parser/parser.go"

	// Seed the semantic index: the entry file is highly similar to the query,
	// the topologically adjacent file is orthogonal and must be pruned by the min score.
	engine := embedx.New(embedx.NewMemoryStore())
	if err := engine.Add(entry, []float32{1, 0, 0}); err != nil {
		t.Fatalf("failed to seed entry vector: %v", err)
	}
	if err := engine.Add(neighbor, []float32{0, 1, 0}); err != nil {
		t.Fatalf("failed to seed neighbor vector: %v", err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				entry:    {Path: entry, Format: "golang", Size: 100, Metadata: map[string]string{}, Entities: []ir.Entity{}},
				neighbor: {Path: neighbor, Format: "golang", Size: 50, Metadata: map[string]string{}, Entities: []ir.Entity{}},
			},
			Links: []ir.Link{
				{SourceID: entry, TargetID: neighbor, Type: "references", Weight: 1.0},
			},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   entry,
		SearchQuery: "explain extractGoData",
		MaxHops:     1,
		MinScore:    0.65,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := responseText(t, resp)
	if !strings.Contains(text, entry) {
		t.Errorf("response should include entry file %q:\n%s", entry, text)
	}
	if strings.Contains(text, neighbor) {
		t.Errorf("response should prune neighbor below min score %q:\n%s", neighbor, text)
	}
	if !strings.Contains(text, "Semantic Score: 1.00") {
		t.Errorf("expected semantic score 1.00 for entry file:\n%s", text)
	}
}

func TestHandleGetNarrowedContext_DefaultsApplied(t *testing.T) {
	const entry = "internal/parser/go-extractor.go"

	engine := embedx.New(embedx.NewMemoryStore())
	if err := engine.Add(entry, []float32{1, 0, 0}); err != nil {
		t.Fatalf("failed to seed entry vector: %v", err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				entry: {Path: entry, Format: "golang", Size: 100, Metadata: map[string]string{}, Entities: []ir.Entity{}},
			},
			Links: []ir.Link{},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	// Zero values for MaxHops/MinScore must default to 1 and 0.65 respectively.
	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   entry,
		SearchQuery: "explain",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), entry) {
		t.Errorf("expected entry file in response:\n%s", responseText(t, resp))
	}
}

func TestHandleGetNarrowedContext_DirectoryEntryExpandsToMembers(t *testing.T) {
	// A directory entry must expand to every indexed file beneath it, and
	// those member files survive the semantic filter even when they score
	// below the min bound (mirroring the single-file entry guarantee).
	engine := embedx.New(embedx.NewMemoryStore())
	if err := engine.Add("src/index.js", []float32{1, 0, 0}); err != nil {
		t.Fatalf("failed to seed entry vector: %v", err)
	}
	if err := engine.Add("src/utils/format.js", []float32{0, 1, 0}); err != nil {
		t.Fatalf("failed to seed neighbor vector: %v", err)
	}
	if err := engine.Add("docs/readme.md", []float32{1, 0, 0}); err != nil {
		t.Fatalf("failed to seed orphan vector: %v", err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				"src/index.js":        {Path: "src/index.js", Format: "javascript", Size: 10, Metadata: map[string]string{}, Entities: []ir.Entity{}},
				"src/utils/format.js": {Path: "src/utils/format.js", Format: "javascript", Size: 10, Metadata: map[string]string{}, Entities: []ir.Entity{}},
				"docs/readme.md":      {Path: "docs/readme.md", Format: "markdown", Size: 10, Metadata: map[string]string{}, Entities: []ir.Entity{}},
			},
			Links: []ir.Link{},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   "src",
		SearchQuery: "explain the app shell",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := responseText(t, resp)
	for _, want := range []string{"src/index.js", "src/utils/format.js"} {
		if !strings.Contains(text, want) {
			t.Errorf("directory entry should surface member %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "docs/readme.md") {
		t.Errorf("directory entry should not include files outside the directory:\n%s", text)
	}
}

func TestHandleGetNarrowedContext_UnknownEntryReturnsError(t *testing.T) {
	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				"src/index.js": {Path: "src/index.js", Format: "javascript", Size: 10, Metadata: map[string]string{}, Entities: []ir.Entity{}},
			},
			Links: []ir.Link{},
		},
		embedEngine: embedx.New(embedx.NewMemoryStore()),
		embedder:    &fakeEmbedder{},
	}

	_, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   "does/not/exist",
		SearchQuery: "explain",
	})
	if err == nil {
		t.Fatal("expected an error for an entry path that matches no indexed file or directory")
	}
}

// failingVectorStore is a VectorStore whose retrieval always fails, used to
// exercise the search-failure branch of handleGetNarrowedContext.
type failingVectorStore struct{}

func (f *failingVectorStore) SaveVector(id string, vec []float32) error {
	return nil
}

func (f *failingVectorStore) GetVector(id string) ([]float32, error) {
	return nil, nil
}

func (f *failingVectorStore) GetAllVectors() (map[string][]float32, error) {
	return nil, errors.New("store is down")
}

func (f *failingVectorStore) Close() error { return nil }

// errEmbedder is a TextEmbedder whose EmbedText always fails.
type errEmbedder struct{}

func (e *errEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	return nil, errors.New("model unavailable")
}

func (e *errEmbedder) Close(ctx context.Context) error { return nil }

func TestHandleGetNarrowedContext_EmbedTextError(t *testing.T) {
	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{"a.go": {Path: "a.go"}},
			Links:     []ir.Link{},
		},
		embedEngine: embedx.New(embedx.NewMemoryStore()),
		embedder:    &errEmbedder{},
	}

	_, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   "a.go",
		SearchQuery: "explain",
	})
	if err == nil || !strings.Contains(err.Error(), "failed to vectorise") {
		t.Errorf("expected vectorisation failure, got: %v", err)
	}
}

func TestHandleGetNarrowedContext_SearchError(t *testing.T) {
	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{"a.go": {Path: "a.go"}},
			Links:     []ir.Link{},
		},
		embedEngine: embedx.New(&failingVectorStore{}),
		embedder:    &fakeEmbedder{},
	}

	_, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   "a.go",
		SearchQuery: "explain",
	})
	if err == nil || !strings.Contains(err.Error(), "vector database query retrieval failed") {
		t.Errorf("expected search failure, got: %v", err)
	}
}

func TestHandleGetNarrowedContext_SkipsLowWeightLinks(t *testing.T) {
	const entry = "internal/parser/go-extractor.go"
	const neighbor = "internal/parser/parser.go"

	engine := embedx.New(embedx.NewMemoryStore())
	// Both files score 1.0 against the query; only topology decides here.
	if err := engine.Add(entry, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Add(neighbor, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				entry:    {Path: entry, Format: "golang"},
				neighbor: {Path: neighbor, Format: "golang"},
			},
			Links: []ir.Link{
				{SourceID: entry, TargetID: neighbor, Type: "references", Weight: 0.4},
			},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   entry,
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(responseText(t, resp), neighbor) {
		t.Errorf("neighbor with weight < 0.5 should not be traversed:\n%s", responseText(t, resp))
	}
}

func TestHandleGetNarrowedContext_TraversesEntityAnchoredLinks(t *testing.T) {
	const entry = "internal/parser/go-extractor.go"
	const neighbor = "internal/parser/parser.go"

	engine := embedx.New(embedx.NewMemoryStore())
	if err := engine.Add(entry, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Add(neighbor, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				entry:    {Path: entry, Format: "golang"},
				neighbor: {Path: neighbor, Format: "golang"},
			},
			// Entity-anchored source (entry#extractGoData) still reaches neighbor.
			Links: []ir.Link{
				{SourceID: entry + "#extractGoData", TargetID: neighbor, Type: "calls", Weight: 1.0},
			},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   entry,
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), neighbor) {
		t.Errorf("entity-anchored link should reach neighbor:\n%s", responseText(t, resp))
	}
}

func TestHandleGetNarrowedContext_FiltersBySourceType(t *testing.T) {
	const entry = "internal/parser/go-extractor.go"
	const extractedNeighbor = "internal/parser/parser.go"
	const inferredNeighbor = "docs/GoExtractor.md"

	engine := embedx.New(embedx.NewMemoryStore())
	// All three files score 1.0 against the query; only topology decides here.
	for _, id := range []string{entry, extractedNeighbor, inferredNeighbor} {
		if err := engine.Add(id, []float32{1, 0, 0}); err != nil {
			t.Fatal(err)
		}
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				entry:             {Path: entry, Format: "golang"},
				extractedNeighbor: {Path: extractedNeighbor, Format: "golang"},
				inferredNeighbor:  {Path: inferredNeighbor, Format: "markdown"},
			},
			Links: []ir.Link{
				{SourceID: entry, TargetID: extractedNeighbor, Type: "calls", Weight: 1.0, SourceType: ir.LinkSourceExtracted},
				{SourceID: inferredNeighbor, TargetID: entry, Type: "documents", Weight: 0.6, SourceType: ir.LinkSourceInferred},
			},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	t.Run("extracted only", func(t *testing.T) {
		resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
			EntryPath:   entry,
			SearchQuery: "explain",
			MaxHops:     1,
			MinScore:    0.65,
			SourceType:  ir.LinkSourceExtracted,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		text := responseText(t, resp)
		if !strings.Contains(text, extractedNeighbor) {
			t.Errorf("extracted link should reach %q:\n%s", extractedNeighbor, text)
		}
		if strings.Contains(text, inferredNeighbor) {
			t.Errorf("inferred link should be filtered out when SourceType=extracted:\n%s", text)
		}
	})

	t.Run("all by default", func(t *testing.T) {
		resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
			EntryPath:   entry,
			SearchQuery: "explain",
			MaxHops:     1,
			MinScore:    0.65,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		text := responseText(t, resp)
		for _, want := range []string{extractedNeighbor, inferredNeighbor} {
			if !strings.Contains(text, want) {
				t.Errorf("expected %q in response (no filter):\n%s", want, text)
			}
		}
	})

	t.Run("legacy untagged links normalize to extracted", func(t *testing.T) {
		legacy := &Server{
			graph: &ir.Graph{
				Documents: map[string]*ir.Document{
					entry:             {Path: entry, Format: "golang"},
					extractedNeighbor: {Path: extractedNeighbor, Format: "golang"},
				},
				Links: []ir.Link{
					// No SourceType: built before the field existed.
					{SourceID: entry, TargetID: extractedNeighbor, Type: "calls", Weight: 1.0},
				},
			},
			embedEngine: engine,
			embedder:    &fakeEmbedder{},
		}
		resp, err := legacy.handleGetNarrowedContext(NarrowContextArgs{
			EntryPath:   entry,
			SearchQuery: "explain",
			MaxHops:     1,
			MinScore:    0.65,
			SourceType:  ir.LinkSourceExtracted,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(responseText(t, resp), extractedNeighbor) {
			t.Errorf("untagged legacy link should match SourceType=extracted:\n%s", responseText(t, resp))
		}
	})
}

func TestHandleGetNarrowedContext_FileReadFailure(t *testing.T) {
	const entry = "nonexistent/doc.go" // present in the graph but not on disk

	engine := embedx.New(embedx.NewMemoryStore())
	if err := engine.Add(entry, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				entry: {Path: entry, Format: "golang"},
			},
			Links: []ir.Link{},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   entry,
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), "[Error reading source code file contents from disk]") {
		t.Errorf("expected file-read failure placeholder:\n%s", responseText(t, resp))
	}
}

func TestHandleGetNarrowedContext_NoResults(t *testing.T) {
	const entry = "ghost.go" // a real indexed document, excluded below

	engine := embedx.New(embedx.NewMemoryStore())
	if err := engine.Add(entry, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Add("some/other.go", []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				entry:           {Path: entry, Format: "golang"},
				"some/other.go": {Path: "some/other.go", Format: "golang"},
			},
			Links: []ir.Link{},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	// Excluding the entry itself (the only file in its frontier) must still
	// surface the explicit no-results message rather than an empty payload.
	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   entry,
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
		Exclude:     []string{entry},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), "No overlapping code structures") {
		t.Errorf("expected no-results message:\n%s", responseText(t, resp))
	}
}

func TestHandleGetDocumentDetails_NoEntitiesOrMetadata(t *testing.T) {
	s := &Server{graph: &ir.Graph{
		Documents: map[string]*ir.Document{
			"README.md": {Path: "README.md", Format: "markdown", Size: 0, Metadata: map[string]string{}, Entities: []ir.Entity{}},
		},
		Links: []ir.Link{},
	}}
	resp, err := s.handleGetDocumentDetails(DocumentQueryArgs{Path: "README.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	if !strings.Contains(text, "No structural entities extracted") {
		t.Errorf("expected no-entities message:\n%s", text)
	}
	if strings.Contains(text, "Document Metadata") {
		t.Errorf("empty metadata should not render a metadata section:\n%s", text)
	}
}

func TestHandleGetDocumentLinks_NoLinks(t *testing.T) {
	s := &Server{graph: &ir.Graph{
		Documents: map[string]*ir.Document{
			"isolated.go": {Path: "isolated.go", Format: "golang"},
		},
		Links: []ir.Link{},
	}}
	resp, err := s.handleGetDocumentLinks(DocumentQueryArgs{Path: "isolated.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	if !strings.Contains(text, "No outgoing references discovered") {
		t.Errorf("expected no outgoing message:\n%s", text)
	}
	if !strings.Contains(text, "No incoming references discovered") {
		t.Errorf("expected no incoming message:\n%s", text)
	}
}

// Table-driven coverage for the format/entity listing handlers: the empty-match
// path and the case-insensitivity path are exercised against a shared fixture.
func TestHandleListDocumentsByFormat_TableDriven(t *testing.T) {
	s := testServer()
	tests := []struct {
		name    string
		format  string
		wantIn  []string
		wantOut []string
		wantMsg string
	}{
		{
			name:    "exact match",
			format:  "golang",
			wantIn:  []string{"store.go", "memstore.go"},
			wantOut: []string{"README.md"},
		},
		{
			name:    "case-insensitive match",
			format:  "MARKDOWN",
			wantIn:  []string{"README.md"},
			wantOut: []string{"store.go"},
		},
		{
			name:    "no match",
			format:  "rust",
			wantMsg: "No registered documents match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := s.handleListDocumentsByFormat(FilterQueryArgs{Format: tt.format})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			text := responseText(t, resp)
			if tt.wantMsg != "" {
				if !strings.Contains(text, tt.wantMsg) {
					t.Errorf("expected %q, got:\n%s", tt.wantMsg, text)
				}
				return
			}
			for _, w := range tt.wantIn {
				if !strings.Contains(text, w) {
					t.Errorf("response missing %q:\n%s", w, text)
				}
			}
			for _, w := range tt.wantOut {
				if strings.Contains(text, w) {
					t.Errorf("response should not contain %q:\n%s", w, text)
				}
			}
		})
	}
}

func TestHandleFindEntitiesByType_TableDriven(t *testing.T) {
	s := testServer()
	tests := []struct {
		name    string
		typ     string
		wantIn  []string
		wantOut []string
		wantMsg string
	}{
		{
			name:    "match interface",
			typ:     "interface",
			wantIn:  []string{"Store"},
			wantOut: []string{"MemStore"},
		},
		{
			name:   "match struct with mixed case input",
			typ:    "STRUCT",
			wantIn: []string{"MemStore"},
		},
		{
			name:    "no match",
			typ:     "person",
			wantMsg: "No extracted elements found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := s.handleFindEntitiesByType(EntityFilterArgs{Type: tt.typ})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			text := responseText(t, resp)
			if tt.wantMsg != "" {
				if !strings.Contains(text, tt.wantMsg) {
					t.Errorf("expected %q, got:\n%s", tt.wantMsg, text)
				}
				return
			}
			for _, w := range tt.wantIn {
				if !strings.Contains(text, w) {
					t.Errorf("response missing %q:\n%s", w, text)
				}
			}
			for _, w := range tt.wantOut {
				if strings.Contains(text, w) {
					t.Errorf("response should not contain %q:\n%s", w, text)
				}
			}
		})
	}
}

func TestHandleGetDocumentDetails_TableDriven(t *testing.T) {
	s := testServer()
	tests := []struct {
		name    string
		path    string
		wantIn  []string
		wantMsg string
	}{
		{
			name:   "document with metadata and entities",
			path:   "internal/store/store.go",
			wantIn: []string{"Document Details:", "tree-sitter", "Store", "interface"},
		},
		{
			name:   "document without entities",
			path:   "README.md",
			wantIn: []string{"Document Details:", "No structural entities extracted"},
		},
		{
			name:    "missing document",
			path:    "does/not/exist.go",
			wantMsg: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := s.handleGetDocumentDetails(DocumentQueryArgs{Path: tt.path})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			text := responseText(t, resp)
			if tt.wantMsg != "" {
				if !strings.Contains(text, tt.wantMsg) {
					t.Errorf("expected %q, got:\n%s", tt.wantMsg, text)
				}
				return
			}
			for _, w := range tt.wantIn {
				if !strings.Contains(text, w) {
					t.Errorf("response missing %q:\n%s", w, text)
				}
			}
		})
	}
}

func TestHandleGetDocumentLinks_TableDriven(t *testing.T) {
	s := testServer()
	tests := []struct {
		name    string
		path    string
		wantIn  []string
		wantMsg string
	}{
		{
			name:   "incoming links for target",
			path:   "internal/store/store.go",
			wantIn: []string{"No outgoing references discovered", "implements", "documents"},
		},
		{
			name:   "outgoing link anchored at entity",
			path:   "internal/store/memstore.go",
			wantIn: []string{"implements"},
		},
		{
			name:    "missing document",
			path:    "missing.go",
			wantMsg: "does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := s.handleGetDocumentLinks(DocumentQueryArgs{Path: tt.path})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			text := responseText(t, resp)
			if tt.wantMsg != "" {
				if !strings.Contains(text, tt.wantMsg) {
					t.Errorf("expected %q, got:\n%s", tt.wantMsg, text)
				}
				return
			}
			for _, w := range tt.wantIn {
				if !strings.Contains(text, w) {
					t.Errorf("response missing %q:\n%s", w, text)
				}
			}
		})
	}
}

func TestHandleGetNarrowedContext_ExcludesPaths(t *testing.T) {
	const entry = "internal/parser/go-extractor.go"
	const excluded = "vendor/dep.go"
	const kept = "internal/parser/parser.go"

	engine := embedx.New(embedx.NewMemoryStore())
	for _, id := range []string{entry, excluded, kept} {
		if err := engine.Add(id, []float32{1, 0, 0}); err != nil {
			t.Fatal(err)
		}
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				entry:    {Path: entry, Format: "golang"},
				excluded: {Path: excluded, Format: "golang"},
				kept:     {Path: kept, Format: "golang"},
			},
			Links: []ir.Link{
				{SourceID: entry, TargetID: excluded, Type: "calls", Weight: 1.0, SourceType: ir.LinkSourceExtracted},
				{SourceID: entry, TargetID: kept, Type: "calls", Weight: 1.0, SourceType: ir.LinkSourceExtracted},
			},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   entry,
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
		Exclude:     []string{"vendor"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	if strings.Contains(text, excluded) {
		t.Errorf("excluded path should not appear in response:\n%s", text)
	}
	if !strings.Contains(text, kept) {
		t.Errorf("kept neighbor should appear in response:\n%s", text)
	}
}

func TestHandleGetNarrowedContext_SnippetSlicing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "widget.go")
	src := `package widget

// top comment

type Widget struct {
	ID   string
	Name string
}

func (w *Widget) GetID() string { return w.ID }

// trailing comment
`
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	engine := embedx.New(embedx.NewMemoryStore())
	if err := engine.Add(path, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				path: {
					Path:   path,
					Format: "golang",
					Entities: []ir.Entity{
						{ID: path + "#Widget", Type: "struct", Name: "Widget", Metadata: map[string]string{"start_line": "5", "end_line": "9"}},
					},
				},
			},
			Links: []ir.Link{},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   path,
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	if !strings.Contains(text, "type Widget struct") {
		t.Errorf("snippet should include struct body:\n%s", text)
	}
	if strings.Contains(text, "trailing comment") {
		t.Errorf("out-of-range content should be sliced out:\n%s", text)
	}
	if !strings.Contains(text, "lines 5-9") {
		t.Errorf("snippet should carry a line range marker:\n%s", text)
	}
}

func TestHandleGetNarrowedContext_TokenBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.go")
	src := "package big\n\nfunc One() {}\n\nfunc Two() {}\n\nfunc Three() {}\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	engine := embedx.New(embedx.NewMemoryStore())
	if err := engine.Add(path, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				path: {Path: path, Format: "golang", Metadata: map[string]string{}, Entities: []ir.Entity{}},
			},
			Links: []ir.Link{},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   path,
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
		MaxTokens:   1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	if !strings.Contains(text, "token budget reached") {
		t.Errorf("expected budget-reached marker:\n%s", text)
	}
	if strings.Contains(text, "func Three()") {
		t.Errorf("budget should have cut the trailing content:\n%s", text)
	}
}

func TestHandleGetNarrowedContext_JSONFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	src := "package p\n\nfunc Alpha() {}\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	engine := embedx.New(embedx.NewMemoryStore())
	if err := engine.Add(path, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				path: {Path: path, Format: "golang", Metadata: map[string]string{}, Entities: []ir.Entity{}},
			},
			Links: []ir.Link{},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   path,
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
		Format:      "json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(responseText(t, resp)), &parsed); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if parsed["entryPath"] != path {
		t.Errorf("entryPath = %v, want %s", parsed["entryPath"], path)
	}
	results, ok := parsed["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("expected non-empty results array, got %v", parsed["results"])
	}
	first := results[0].(map[string]any)
	if _, ok := first["snippets"]; !ok {
		t.Errorf("expected snippets array in result, got %v", first)
	}
}

func TestHandleGetNarrowedContext_PackageExpansion(t *testing.T) {
	const entry = "internal/ir/types.go"
	const sibling = "internal/ir/helpers.go"
	const dep = "internal/analyzer/analyzer.go"

	engine := embedx.New(embedx.NewMemoryStore())
	for _, id := range []string{entry, sibling, dep} {
		if err := engine.Add(id, []float32{1, 0, 0}); err != nil {
			t.Fatal(err)
		}
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				entry:   {Path: entry, Format: "golang"},
				sibling: {Path: sibling, Format: "golang"},
				dep:     {Path: dep, Format: "golang"},
			},
			Links: []ir.Link{
				{SourceID: entry, TargetID: ir.PackageNodeID("internal/ir"), Type: "part_of", Weight: 1.0, SourceType: ir.LinkSourceExtracted},
				{SourceID: entry, TargetID: ir.PackageNodeID("internal/analyzer"), Type: "imports", Weight: 0.8, SourceType: ir.LinkSourceInferred},
			},
			Packages: map[string]*ir.Package{
				"internal/ir":       {Name: "ir", Path: "internal/ir", Files: []string{entry, sibling}},
				"internal/analyzer": {Name: "analyzer", Path: "internal/analyzer", Files: []string{dep}},
			},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   entry,
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	// One hop from the entry reaches both the package sibling (via part_of)
	// and the imported package's file (via the imports edge expanding to the
	// whole dependency package).
	for _, want := range []string{sibling, dep} {
		if !strings.Contains(text, want) {
			t.Errorf("expected package-expanded neighbor %q in response:\n%s", want, text)
		}
	}
}

func TestHandleGetNarrowedContext_StalenessNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	src := "package p\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	engine := embedx.New(embedx.NewMemoryStore())
	if err := engine.Add(path, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				path: {Path: path, Format: "golang", Metadata: map[string]string{}, Entities: []ir.Entity{}, UpdatedAt: time.Now()},
			},
			Links:   []ir.Link{},
			BuiltAt: time.Now().Add(-time.Hour),
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   path,
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	if !strings.Contains(text, "Graph snapshot:") {
		t.Errorf("expected snapshot timestamp line:\n%s", text)
	}
	if !strings.Contains(text, "STALE") {
		t.Errorf("expected staleness marker for a file newer than the snapshot:\n%s", text)
	}
}

func TestHandleListClusters_AllKinds(t *testing.T) {
	s := testServer()
	resp, err := s.handleListClusters(ClusterListArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	for _, want := range []string{
		"directory:internal/store",
		"directory:.",
		"network:internal/store/store.go",
		"2 files",
		"3 files",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("response missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "#### network") {
		t.Errorf("expected network cluster section:\n%s", text)
	}
}

func TestHandleListClusters_KindFilter(t *testing.T) {
	s := testServer()
	resp, err := s.handleListClusters(ClusterListArgs{Kind: "directory"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	if !strings.Contains(text, "directory:internal/store") {
		t.Errorf("expected directory cluster in filtered response:\n%s", text)
	}
	if strings.Contains(text, "network:") {
		t.Errorf("kind filter should exclude network clusters:\n%s", text)
	}
}

func TestHandleListClusters_NoClusters(t *testing.T) {
	s := &Server{graph: &ir.Graph{
		Documents: map[string]*ir.Document{"a.go": {Path: "a.go"}},
		Links:     []ir.Link{},
	}}
	resp, err := s.handleListClusters(ClusterListArgs{Kind: "network"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), "No clusters found") {
		t.Errorf("expected no-clusters message, got: %s", responseText(t, resp))
	}
}

func TestHandleListClusters_NilGraph(t *testing.T) {
	s := &Server{graph: nil}
	_, err := s.handleListClusters(ClusterListArgs{})
	if err == nil || !strings.Contains(err.Error(), "uninitialized") {
		t.Errorf("expected uninitialized error, got: %v", err)
	}
}

func TestHandleGetCluster_Found(t *testing.T) {
	s := testServer()
	resp, err := s.handleGetCluster(ClusterDetailArgs{ID: "directory:internal/store"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	for _, want := range []string{
		"Cluster: `directory:internal/store`",
		"**Kind:** directory",
		"**Size:** 2 files",
		"internal/store/memstore.go",
		"internal/store/store.go",
		"(golang)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("response missing %q:\n%s", want, text)
		}
	}
}

func TestHandleGetCluster_NotFound(t *testing.T) {
	s := testServer()
	resp, err := s.handleGetCluster(ClusterDetailArgs{ID: "directory:does/not/exist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), "not found") {
		t.Errorf("expected not-found message, got: %s", responseText(t, resp))
	}
}

func TestHandleGetDocumentDetails_ListsMemberClusters(t *testing.T) {
	s := testServer()
	resp, err := s.handleGetDocumentDetails(DocumentQueryArgs{Path: "internal/store/store.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	if !strings.Contains(text, "Member of Clusters") {
		t.Errorf("expected cluster membership section:\n%s", text)
	}
	if !strings.Contains(text, "directory:internal/store") {
		t.Errorf("expected directory cluster membership:\n%s", text)
	}
}

func TestPreviewMembers(t *testing.T) {
	got := previewMembers([]string{"a.go", "b.go", "c.go", "d.go"}, 2)
	if got != "`a.go`, `b.go`, …" {
		t.Errorf("previewMembers = %q, want truncated preview", got)
	}
	got = previewMembers([]string{"a.go", "b.go"}, 5)
	if got != "`a.go`, `b.go`" {
		t.Errorf("previewMembers = %q, want full preview", got)
	}
	got = previewMembers(nil, 3)
	if got != "none" {
		t.Errorf("previewMembers(nil) = %q, want none", got)
	}
}

// metricsServer builds a Server whose graph carries a populated Metrics
// table: hub.go is the most central document (degree 4), leaves have degree
// 1, and one isolated doc stays at zero.
func metricsServer() *Server {
	hub := ir.DocumentMetrics{Degree: 4, WeightedDegree: 40, PageRank: 0.5, IsHub: true}
	leaf := ir.DocumentMetrics{Degree: 1, WeightedDegree: 10, PageRank: 0.125}
	iso := ir.DocumentMetrics{}
	return &Server{graph: &ir.Graph{
		Documents: map[string]*ir.Document{
			"hub.go":      {Path: "hub.go", Format: "golang", Metadata: map[string]string{ir.MetadataHubFlag: "true"}},
			"leaf.go":     {Path: "leaf.go", Format: "golang", Metadata: map[string]string{}},
			"isolated.md": {Path: "isolated.md", Format: "markdown", Metadata: map[string]string{}},
		},
		Links: []ir.Link{},
		Metrics: ir.Metrics{
			Documents: map[string]ir.DocumentMetrics{
				"hub.go":      hub,
				"leaf.go":     leaf,
				"isolated.md": iso,
			},
			HubCount: 1,
		},
	}}
}

func TestHandleGetGraphMetrics_HubsFirst(t *testing.T) {
	s := metricsServer()
	resp, err := s.handleGetGraphMetrics(MetricsArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	for _, want := range []string{
		"- **Documents indexed:** 3",
		"- **Hub count:** 1",
		"[HUB] `hub.go`",
		"degree 4",
		"weighted 40.00",
		"PageRank 0.50000",
		"`leaf.go`",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("response missing %q:\n%s", want, text)
		}
	}
	// Hubs must sort ahead of non-hub documents.
	hubPos := strings.Index(text, "[HUB]")
	leafPos := strings.Index(text, "`leaf.go`")
	if hubPos < 0 || leafPos < 0 || hubPos > leafPos {
		t.Errorf("hub should precede leaf in report (hub=%d leaf=%d):\n%s", hubPos, leafPos, text)
	}
}

func TestHandleGetGraphMetrics_MaxResults(t *testing.T) {
	s := metricsServer()
	resp, err := s.handleGetGraphMetrics(MetricsArgs{MaxResults: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	if !strings.Contains(text, "[HUB] `hub.go`") {
		t.Errorf("expected hub in capped report:\n%s", text)
	}
	if strings.Contains(text, "`leaf.go`") {
		t.Errorf("MaxResults=1 should omit leaf.go:\n%s", text)
	}
}

func TestHandleGetGraphMetrics_NoMetrics(t *testing.T) {
	s := &Server{graph: &ir.Graph{
		Documents: map[string]*ir.Document{"a.go": {Path: "a.go"}},
		Links:     []ir.Link{},
	}}
	resp, err := s.handleGetGraphMetrics(MetricsArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), "no centrality metrics") {
		t.Errorf("expected no-metrics message, got: %s", responseText(t, resp))
	}
}

func TestHandleGetGraphMetrics_NilGraph(t *testing.T) {
	s := &Server{graph: nil}
	_, err := s.handleGetGraphMetrics(MetricsArgs{})
	if err == nil || !strings.Contains(err.Error(), "uninitialized") {
		t.Errorf("expected uninitialized error, got: %v", err)
	}
}

func TestHandleGetDocumentDetails_ShowsMetrics(t *testing.T) {
	s := metricsServer()
	resp, err := s.handleGetDocumentDetails(DocumentQueryArgs{Path: "hub.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	for _, want := range []string{
		"Centrality Metrics (God Node)",
		"**Hub:** YES",
		"**Degree:** 4",
		"**Weighted Degree:** 40.00",
		"**PageRank:** 0.50000",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("response missing %q:\n%s", want, text)
		}
	}
}

func TestHandleGetDocumentDetails_NonHubMetrics(t *testing.T) {
	s := metricsServer()
	resp, err := s.handleGetDocumentDetails(DocumentQueryArgs{Path: "leaf.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := responseText(t, resp)
	if !strings.Contains(text, "**Hub:** no") {
		t.Errorf("expected non-hub marker, got:\n%s", text)
	}
	if !strings.Contains(text, "**Degree:** 1") {
		t.Errorf("expected degree 1 for leaf, got:\n%s", text)
	}
}

func TestHandleGetNarrowedContext_HubMarker(t *testing.T) {
	const entry = "hub.go"

	engine := embedx.New(embedx.NewMemoryStore())
	if err := engine.Add(entry, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}

	s := &Server{
		graph: &ir.Graph{
			Documents: map[string]*ir.Document{
				entry: {Path: entry, Format: "golang", Metadata: map[string]string{}},
			},
			Links: []ir.Link{},
			Metrics: ir.Metrics{
				Documents: map[string]ir.DocumentMetrics{
					entry: {Degree: 4, WeightedDegree: 40, PageRank: 0.5, IsHub: true},
				},
				HubCount: 1,
			},
		},
		embedEngine: engine,
		embedder:    &fakeEmbedder{},
	}

	resp, err := s.handleGetNarrowedContext(NarrowContextArgs{
		EntryPath:   entry,
		SearchQuery: "explain",
		MaxHops:     1,
		MinScore:    0.65,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(responseText(t, resp), "God Node):** YES") {
		t.Errorf("expected hub marker in narrowed context:\n%s", responseText(t, resp))
	}
}
