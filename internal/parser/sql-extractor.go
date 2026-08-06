package parser

import (
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractSQLData parses a SQL file, pulling out tables, views, indexes,
// functions, and within-file "references" links (view -> table,
// index -> table, foreign key -> table).
func extractSQLData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, sqlConfig())
}

// sqlConfig maps the tree-sitter-sql grammar onto the shared walker.
// Notes on the grammar:
//
//   - CREATE TABLE / VIEW / FUNCTION put the object name in a bare
//     identifier child, so NameNodeType finds it (CREATE INDEX alone
//     uses a "name" field).
//   - Foreign keys are references_constraint descendants whose first
//     identifier names the referenced table.
//   - View bodies reference tables through plain identifier nodes inside
//     the from/join clauses; the InspectHook resolves those names against
//     the tables declared in this file and emits "references" links.
//
// Insert/update targets are skipped: those statements have no entity of
// their own to source a within-file link from.
func sqlConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.SqlLanguage(),
		Declarations: []DeclarationSpec{
			{
				NodeType:     "create_table_statement",
				Kind:         "table",
				NameNodeType: "identifier",
			},
			{
				NodeType:     "create_view_statement",
				Kind:         "view",
				NameNodeType: "identifier",
			},
			{
				NodeType:  "create_index_statement",
				Kind:      "index",
				NameField: "name",
			},
			{
				NodeType:     "create_function_statement",
				Kind:         "function",
				NameNodeType: "identifier",
			},
		},
		InspectHook: sqlInspectHook,
	}
}

// sqlInspectHook emits within-file "references" links from a declared
// SQL object (table, view, index) to tables it depends on.
func sqlInspectHook(w *Walker, n *sitter.Node) {
	srcID := w.declarationEntityID(n)
	if srcID == "" {
		return
	}

	var candidates []string
	switch n.Type(w.lang()) {
	case "create_table_statement":
		// Foreign keys: each references_constraint names a table.
		for _, rc := range w.descendantsOfType(n, "references_constraint") {
			if id := w.firstDescendantOfType(rc, "identifier"); id != nil {
				candidates = append(candidates, strings.TrimSpace(string(w.content[id.StartByte():id.EndByte()])))
			}
		}
	case "create_view_statement":
		// Every identifier inside the view body is a candidate; only
		// names that match a declared table resolve.
		if body := w.childOfType(n, "view_body"); body != nil {
			candidates = w.descendantTextsOfType(body, "identifier")
		}
	case "create_index_statement":
		if tn := n.ChildByFieldName("table_name", w.lang()); tn != nil {
			candidates = append(candidates, strings.TrimSpace(string(w.content[tn.StartByte():tn.EndByte()])))
		}
	default:
		return
	}

	seen := make(map[string]bool)
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		target, ok := w.declared[c]
		if !ok || target == srcID {
			continue
		}
		seen[c] = true
		w.links = append(w.links, ir.Link{
			SourceID:   srcID,
			TargetID:   target,
			Type:       "references",
			Weight:     1.0,
			SourceType: ir.LinkSourceExtracted,
		})
	}
}
