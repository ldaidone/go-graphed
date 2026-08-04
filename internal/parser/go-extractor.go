package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractGoData uses tree-sitter to parse Go source and pull out type
// declarations (structs and interfaces), functions, methods, imports,
// and a within-file call graph. Tree-sitter is chosen over go/ast
// because it is resilient to syntax errors and can be extended to
// other grammars without a full compiler.
func extractGoData(path string) ([]ir.Entity, []ir.Link, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read file: %w", err)
	}

	lang := grammars.GoLanguage()
	// Initialize pure-Go tree-sitter parser for Go
	parser := sitter.NewParser(lang)

	tree, err := parser.Parse(content)
	if err != nil {
		return nil, nil, fmt.Errorf("tree-sitter failed parsing: %w", err)
	}

	var entities []ir.Entity
	var links []ir.Link

	// The package_clause is a direct child of the source_file root in the
	// Go grammar. Its name is a hard fact (parsed, not guessed), and the
	// directory it lives in lets the analyzer group sibling files into a
	// package node for cross-file indexing.
	if pkgClause := findChildNodeByType(lang, tree.RootNode(), "package_clause"); pkgClause != nil {
		if name := findChildNodeByType(lang, pkgClause, "package_identifier"); name != nil {
			entities = append(entities, ir.Entity{
				ID:   fmt.Sprintf("%s#package", path),
				Type: "package",
				Name: string(content[name.StartByte():name.EndByte()]),
				Metadata: map[string]string{
					"package_path": filepath.Dir(path),
					"start_line":   fmt.Sprintf("%d", pkgClause.StartPoint().Row+1),
					"end_line":     fmt.Sprintf("%d", pkgClause.EndPoint().Row+1),
				},
			})
		}
	}

	// declared maps every function/method symbol to its entity ID so
	// call sites resolve even when they appear earlier in the file.
	declared := make(map[string]string)

	// Pass 1: register all function and method symbols.  Runs before
	// entity extraction so forward references produce "calls" links.
	var registerSymbols func(*sitter.Node)
	registerSymbols = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Type(lang) {
		case "function_declaration":
			if name := n.ChildByFieldName("name", lang); name != nil {
				funcName := string(content[name.StartByte():name.EndByte()])
				declared[funcName] = fmt.Sprintf("%s#function:%s", path, funcName)
			}
		case "method_declaration":
			if name := n.ChildByFieldName("name", lang); name != nil {
				methodName := string(content[name.StartByte():name.EndByte()])
				fullName := receiverTypeName(lang, n.ChildByFieldName("receiver", lang), content) + "." + methodName
				declared[fullName] = fmt.Sprintf("%s#method:%s", path, fullName)
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			registerSymbols(n.Child(i))
		}
	}
	registerSymbols(tree.RootNode())

	// currentFunc tracks the entity ID of the enclosing function or
	// method, so call_expression nodes know who the caller is.
	var currentFunc []string

	// Pass 2: extract entities and resolve calls. Using a closure lets
	// the walker share the state slices without threading them through
	// function arguments at every recursion level.
	var inspectNode func(*sitter.Node)
	inspectNode = func(n *sitter.Node) {
		if n == nil {
			return
		}

		switch n.Type(lang) {
		case "type_spec":
			nameNode := n.ChildByFieldName("name", lang)
			typeNode := n.ChildByFieldName("type", lang)

			if nameNode != nil && typeNode != nil {
				entityName := string(content[nameNode.StartByte():nameNode.EndByte()])
				nodeTypeStr := typeNode.Type(lang) // "struct_type" or "interface_type"

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

		case "function_declaration":
			if name := n.ChildByFieldName("name", lang); name != nil {
				funcName := string(content[name.StartByte():name.EndByte()])
				id := fmt.Sprintf("%s#function:%s", path, funcName)
				entities = append(entities, ir.Entity{
					ID:   id,
					Type: "function",
					Name: funcName,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
				currentFunc = append(currentFunc, id)
				for i := 0; i < int(n.ChildCount()); i++ {
					inspectNode(n.Child(i))
				}
				currentFunc = currentFunc[:len(currentFunc)-1]
			}
			return

		case "method_declaration":
			if name := n.ChildByFieldName("name", lang); name != nil {
				methodName := string(content[name.StartByte():name.EndByte()])
				fullName := receiverTypeName(lang, n.ChildByFieldName("receiver", lang), content) + "." + methodName
				id := fmt.Sprintf("%s#method:%s", path, fullName)
				entities = append(entities, ir.Entity{
					ID:   id,
					Type: "method",
					Name: fullName,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
				currentFunc = append(currentFunc, id)
				for i := 0; i < int(n.ChildCount()); i++ {
					inspectNode(n.Child(i))
				}
				currentFunc = currentFunc[:len(currentFunc)-1]
			}
			return

		case "import_spec":
			if p := n.ChildByFieldName("path", lang); p != nil {
				importPath := unquoteString(string(content[p.StartByte():p.EndByte()]))
				entities = append(entities, ir.Entity{
					ID:   fmt.Sprintf("%s#import:%s", path, importPath),
					Type: "import",
					Name: importPath,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
			}
			return

		case "call_expression":
			// Simple identifier calls to locally-declared symbols become
			// "calls" links. Selector calls (pkg.Fn, recv.Method) are
			// left out of the within-file graph.
			if fn := n.ChildByFieldName("function", lang); fn != nil && len(currentFunc) > 0 && fn.Type(lang) == "identifier" {
				callee := string(content[fn.StartByte():fn.EndByte()])
				if target, ok := declared[callee]; ok {
					links = append(links, ir.Link{
						SourceID:   currentFunc[len(currentFunc)-1],
						TargetID:   target,
						Type:       "calls",
						Weight:     1.0,
						SourceType: ir.LinkSourceExtracted,
					})
				}
			}
			// Fall through so nested call expressions are visited too.
		}

		for i := 0; i < int(n.ChildCount()); i++ {
			inspectNode(n.Child(i))
		}
	}

	inspectNode(tree.RootNode())
	return entities, links, nil
}

// receiverTypeName extracts the receiver type from a method
// declaration's receiver parameter list, e.g. "(d *Dog)" -> "Dog".
func receiverTypeName(lang *sitter.Language, recv *sitter.Node, content []byte) string {
	if recv == nil {
		return ""
	}
	text := strings.TrimSpace(string(content[recv.StartByte():recv.EndByte()]))
	text = strings.TrimPrefix(text, "(")
	text = strings.TrimSuffix(text, ")")
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimPrefix(fields[len(fields)-1], "*")
}

// findChildNodeByType scans a node's direct children for the first one
// with the given tree-sitter type. It deliberately does not recurse so
// structural declarations at the top of a file are found cheaply.
func findChildNodeByType(lang *sitter.Language, parent *sitter.Node, wantType string) *sitter.Node {
	if parent == nil {
		return nil
	}
	for i := 0; i < int(parent.ChildCount()); i++ {
		if child := parent.Child(i); child.Type(lang) == wantType {
			return child
		}
	}
	return nil
}
