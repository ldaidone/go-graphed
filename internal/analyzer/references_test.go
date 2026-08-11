package analyzer

import (
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
)

func refDoc(path string, refs, imports []string) ir.Document {
	d := ir.Document{Path: path, Format: "kotlin", Metadata: map[string]string{}}
	for i, name := range refs {
		d.Entities = append(d.Entities, ir.Entity{
			ID:   path + "#reference:" + name,
			Type: "reference",
			Name: name,
		})
		_ = i
	}
	for _, name := range imports {
		d.Entities = append(d.Entities, ir.Entity{
			ID:   path + "#import:" + name,
			Type: "import",
			Name: name,
		})
	}
	return d
}

func classDoc(path, className string) ir.Document {
	return ir.Document{
		Path:     path,
		Format:   "kotlin",
		Metadata: map[string]string{},
		Entities: []ir.Entity{
			{ID: path + "#class:" + className, Type: "class", Name: className},
		},
	}
}

func referencesLinks(t *testing.T, docs []ir.Document) []ir.Link {
	t.Helper()
	graph, err := Build(docs)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	var links []ir.Link
	for _, link := range graph.Links {
		if link.Type == "references" {
			links = append(links, link)
		}
	}
	return links
}

func TestBuild_ReferenceImportContextDisambiguation(t *testing.T) {
	// OrderController imports the service package, so its reference to
	// OrderService must resolve to service/OrderService.kt, not to the
	// unrelated OrderService.kt elsewhere.
	docs := []ir.Document{
		refDoc("controller/OrderController.kt", []string{"OrderService"}, []string{"com.acme.service.OrderService"}),
		classDoc("service/OrderService.kt", "OrderService"),
		classDoc("legacy/OrderService.kt", "OrderService"),
	}

	links := referencesLinks(t, docs)
	if len(links) != 1 {
		t.Fatalf("expected 1 references edge, got %d: %v", len(links), links)
	}
	if !strings.HasPrefix(links[0].TargetID, "service/OrderService.kt") {
		t.Errorf("reference resolved to %q, want service/OrderService.kt", links[0].TargetID)
	}
	if links[0].SourceType != ir.LinkSourceInferred || links[0].Weight != 0.6 {
		t.Errorf("references edge SourceType=%q Weight=%v, want inferred 0.6", links[0].SourceType, links[0].Weight)
	}
}

func TestBuild_ReferenceSelfReferenceSkipped(t *testing.T) {
	// A type declared in the same file as the reference must not produce a
	// self-reference edge.
	docs := []ir.Document{
		{
			Path:     "controller/App.kt",
			Format:   "kotlin",
			Metadata: map[string]string{},
			Entities: []ir.Entity{
				{ID: "controller/App.kt#class:OrderController", Type: "class", Name: "OrderController"},
				{ID: "controller/App.kt#reference:OrderController", Type: "reference", Name: "OrderController"},
			},
		},
	}

	if links := referencesLinks(t, docs); len(links) != 0 {
		t.Errorf("self-references produced edges: %v", links)
	}
}

func TestBuild_ReferenceAmbiguousWithoutImportContextSkipped(t *testing.T) {
	// Two files declare Helper and the referencing file imports neither:
	// the mention is ambiguous and must stay unlinked.
	docs := []ir.Document{
		refDoc("a/Use.kt", []string{"Helper"}, nil),
		classDoc("b/Helper.kt", "Helper"),
		classDoc("c/Helper.kt", "Helper"),
	}

	if links := referencesLinks(t, docs); len(links) != 0 {
		t.Errorf("ambiguous reference produced edges: %v", links)
	}
}

func TestBuild_ReferenceUniqueNameFallsBackToGlobal(t *testing.T) {
	// A globally unique name resolves even without import context.
	docs := []ir.Document{
		refDoc("app/Draw.kt", []string{"GeometryUtil"}, nil),
		classDoc("lib/GeometryUtil.kt", "GeometryUtil"),
	}

	links := referencesLinks(t, docs)
	if len(links) != 1 {
		t.Fatalf("expected 1 references edge, got %d: %v", len(links), links)
	}
	if !strings.HasPrefix(links[0].TargetID, "lib/GeometryUtil.kt") {
		t.Errorf("reference resolved to %q, want lib/GeometryUtil.kt", links[0].TargetID)
	}
}
