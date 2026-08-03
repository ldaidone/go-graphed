package mcp

import (
	"context"
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
			{SourceID: "internal/store/memstore.go#MemStore", TargetID: "internal/store/store.go#Store", Type: "implements", Weight: 0.8},
			{SourceID: "README.md", TargetID: "internal/store/store.go#Store", Type: "documents", Weight: 1.0},
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
