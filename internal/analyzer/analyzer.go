// Package analyzer takes the flat list of parsed Documents and
// assembles them into a Graph, including cross-reference links
// inferred by heuristic rules.  Keeping heuristics here (rather
// than in each parser) means the same link logic applies
// regardless of which language extracted the entities.
package analyzer

import (
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
)

// Build maps the parsed document slice into the final ir.Graph
// structure and connects them with inferred links.  Returning a
// Graph (rather than writing to a file) keeps the analyzer
// side-effect-free so it can be tested with in-memory fixtures.
func Build(docs []ir.Document) (ir.Graph, error) {
	graph := ir.Graph{
		Documents: make(map[string]*ir.Document),
		Links:     make([]ir.Link, 0),
	}

	// 1. Map all documents by their Path so the analyzer can
	//    resolve source/target references in O(1).
	for i := range docs {
		doc := &docs[i]
		graph.Documents[doc.Path] = doc
	}

	// 2. Build a global registry of all discovered Entities.
	//    Keying by Name (rather than ID) lets us do substring
	//    matching for the naming-convention heuristic below.
	globalEntities := make(map[string]ir.Entity)
	for _, doc := range graph.Documents {
		for _, entity := range doc.Entities {
			globalEntities[entity.Name] = entity
		}
	}

	// 3. Connect the Dots -- run every heuristic pass over the
	//    full document set.  Each pass is self-contained so new
	//    rules can be added without touching existing ones.
	for _, doc := range graph.Documents {

		// A. Implicit Code-to-Code Links: Does a struct name end
		//    with an interface name?  This is a rough heuristic for
		//    Go's implicit interface satisfaction -- e.g., a struct
		//    called "GraphBuilder" likely implements an interface
		//    called "Builder".  The 0.8 weight signals confidence
		//    is high but not absolute (naming coincidences exist).
		for _, entity := range doc.Entities {
			if entity.Type == "struct" {
				for globalName, globalEntity := range globalEntities {
					if globalEntity.Type == "interface" && strings.HasSuffix(entity.Name, globalName) {
						graph.Links = append(graph.Links, ir.Link{
							SourceID: entity.ID,
							TargetID: globalEntity.ID,
							Type:     "implements",
							Weight:   0.8,
							Metadata: map[string]string{
								"rule": "naming_convention_heuristic",
							},
						})
					}
				}
			}
		}

		// B. Unstructured Context Links: If a Markdown or PDF
		//    document's file path contains an entity name, link
		//    them as "documents".  This is a stand-in for full-text
		//    search -- when file content parsing is added, the same
		//    pattern can scan body text instead of just the path.
		if doc.Format == "markdown" || doc.Format == "pdf" {
			for name, entity := range globalEntities {
				if strings.Contains(strings.ToLower(doc.Path), strings.ToLower(name)) {
					graph.Links = append(graph.Links, ir.Link{
						SourceID: doc.Path,
						TargetID: entity.ID,
						Type:     "documents",
						Weight:   1.0,
						Metadata: map[string]string{
							"rule": "path_keyword_match",
						},
					})
				}
			}
		}

		// C. Document-internal links: parsers can attach links they
		//    discovered inside a single file (e.g. a Go call graph).
		//    These are lifted onto the graph as-is; the analyzer does
		//    not re-infer or re-weight them.
		graph.Links = append(graph.Links, doc.Links...)
	}

	return graph, nil
}
