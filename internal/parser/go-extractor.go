package parser

import (
	"context"
	"fmt"
	"os"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// extractGoData uses tree-sitter to parse Go source and pull out
// type declarations (structs and interfaces).  Tree-sitter is
// chosen over go/ast because it is resilient to syntax errors
// and can be extended to other grammars without a full compiler.
func extractGoData(path string) ([]ir.Entity, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}

	// Initialize tree-sitter parser for Go
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter failed parsing: %w", err)
	}

	var entities []ir.Entity

	// Recursive AST walker helper.  Using a closure lets the walker
	// share the `entities` slice without threading it through
	// function arguments at every recursion level.
	var inspectNode func(*sitter.Node)
	inspectNode = func(n *sitter.Node) {
		if n == nil {
			return
		}

		// We only care about type_spec nodes because that is where
		// Go defines structs and interfaces -- the two entity kinds
		// the analyzer can currently reason about.
		if n.Type() == "type_spec" {
			nameNode := n.ChildByFieldName("name")
			typeNode := n.ChildByFieldName("type")

			if nameNode != nil && typeNode != nil {
				entityName := string(content[nameNode.StartByte():nameNode.EndByte()])
				nodeTypeStr := typeNode.Type() // "struct_type" or "interface_type"

				if nodeTypeStr == "struct_type" || nodeTypeStr == "interface_type" {
					kind := "struct"
					if nodeTypeStr == "interface_type" {
						kind = "interface"
					}

					entities = append(entities, ir.Entity{
						// ID embeds the file path so it is globally unique
						// across the entire codebase without a separate registry.
						ID:   fmt.Sprintf("%s#%s", path, entityName),
						Type: kind,
						Name: entityName,
						Metadata: map[string]string{
							"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
							"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
						},
					})
				}
			}
		}

		// Keep traversing down the tree branches
		for i := 0; i < int(n.ChildCount()); i++ {
			inspectNode(n.Child(i))
		}
	}

	inspectNode(tree.RootNode())
	return entities, nil
}
