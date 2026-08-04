package parser

import (
	"fmt"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractTSData parses a TypeScript file, adding the type-level
// declarations (interfaces, type aliases, enums) on top of the JS set.
func extractTSData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractJSish(path, grammars.TypescriptLanguage(), tsExtra)
}

// extractTSXData parses a TSX file with the TypeScript rule set.
func extractTSXData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractJSish(path, grammars.TsxLanguage(), tsExtra)
}

// tsExtra extends the shared JS-family walker with the TypeScript-only
// declaration kinds. Interfaces, type aliases, and enums are registered
// as symbols during pass 1 so named exports resolve, then extracted as
// entities during pass 2.
var tsExtra = extraNodeHandler{
	declareSymbols: func(w *jsishWalker, n *sitter.Node) {
		switch n.Type(w.lang) {
		case "interface_declaration":
			if name := n.ChildByFieldName("name", w.lang); name != nil {
				declName := string(w.content[name.StartByte():name.EndByte()])
				w.declared[declName] = fmt.Sprintf("%s#interface:%s", w.path, declName)
			}
		case "type_alias_declaration":
			if name := n.ChildByFieldName("name", w.lang); name != nil {
				declName := string(w.content[name.StartByte():name.EndByte()])
				w.declared[declName] = fmt.Sprintf("%s#type-alias:%s", w.path, declName)
			}
		case "enum_declaration":
			if name := n.ChildByFieldName("name", w.lang); name != nil {
				declName := string(w.content[name.StartByte():name.EndByte()])
				w.declared[declName] = fmt.Sprintf("%s#enum:%s", w.path, declName)
			}
		}
	},
	extractEntities: func(w *jsishWalker, n *sitter.Node) {
		switch n.Type(w.lang) {
		case "interface_declaration":
			if name := n.ChildByFieldName("name", w.lang); name != nil {
				declName := string(w.content[name.StartByte():name.EndByte()])
				w.entities = append(w.entities, ir.Entity{
					ID:   fmt.Sprintf("%s#interface:%s", w.path, declName),
					Type: "interface",
					Name: declName,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
			}
		case "type_alias_declaration":
			if name := n.ChildByFieldName("name", w.lang); name != nil {
				declName := string(w.content[name.StartByte():name.EndByte()])
				w.entities = append(w.entities, ir.Entity{
					ID:   fmt.Sprintf("%s#type-alias:%s", w.path, declName),
					Type: "type-alias",
					Name: declName,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
			}
		case "enum_declaration":
			if name := n.ChildByFieldName("name", w.lang); name != nil {
				declName := string(w.content[name.StartByte():name.EndByte()])
				w.entities = append(w.entities, ir.Entity{
					ID:   fmt.Sprintf("%s#enum:%s", w.path, declName),
					Type: "enum",
					Name: declName,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
			}
		}
	},
}
