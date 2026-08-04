package parser

import (
	"fmt"
	"os"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractTOMLData uses tree-sitter to parse TOML and pull out the
// tables and key/value pairs that structure a config file.
func extractTOMLData(path string) ([]ir.Entity, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}

	lang := grammars.TomlLanguage()
	parser := sitter.NewParser(lang)

	tree, err := parser.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter failed parsing: %w", err)
	}

	var entities []ir.Entity
	// currentTable names the enclosing [table] header so pair names
	// stay unique when the same key appears under different tables.
	var currentTable string

	var inspectNode func(*sitter.Node)
	inspectNode = func(n *sitter.Node) {
		if n == nil {
			return
		}

		switch n.Type(lang) {
		case "table":
			nameNode := tomlKeyNode(lang, n)
			if nameNode != nil {
				currentTable = strings.TrimSpace(string(content[nameNode.StartByte():nameNode.EndByte()]))
				entities = append(entities, ir.Entity{
					ID:   fmt.Sprintf("%s#table:%s", path, currentTable),
					Type: "table",
					Name: currentTable,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				inspectNode(n.Child(i))
			}
			return

		case "pair":
			keyNode := tomlKeyNode(lang, n)
			if keyNode != nil {
				keyName := strings.TrimSpace(string(content[keyNode.StartByte():keyNode.EndByte()]))
				fullName := keyName
				if currentTable != "" {
					fullName = currentTable + "." + keyName
				}

				// The value is the last child of the pair (key, "=", value).
				var valueNode *sitter.Node
				if n.ChildCount() > 0 {
					valueNode = n.Child(int(n.ChildCount()) - 1)
				}
				if valueNode != nil && valueNode.Type(lang) == "array" {
					entities = append(entities, ir.Entity{
						ID:   fmt.Sprintf("%s#array:%s", path, fullName),
						Type: "array",
						Name: fullName,
						Metadata: map[string]string{
							"start_line": fmt.Sprintf("%d", valueNode.StartPoint().Row+1),
							"end_line":   fmt.Sprintf("%d", valueNode.EndPoint().Row+1),
						},
					})
				} else {
					entities = append(entities, ir.Entity{
						ID:   fmt.Sprintf("%s#property:%s", path, fullName),
						Type: "property",
						Name: fullName,
						Metadata: map[string]string{
							"start_line": fmt.Sprintf("%d", keyNode.StartPoint().Row+1),
							"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
						},
					})
				}
			}
			return
		}

		for i := 0; i < int(n.ChildCount()); i++ {
			inspectNode(n.Child(i))
		}
	}

	inspectNode(tree.RootNode())
	return entities, nil
}

// tomlKeyNode returns the key node (bare_key, dotted_key, or quoted_key)
// of a TOML table header or pair.
func tomlKeyNode(lang *sitter.Language, n *sitter.Node) *sitter.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		switch n.Child(i).Type(lang) {
		case "bare_key", "dotted_key", "quoted_key":
			return n.Child(i)
		}
	}
	return nil
}
