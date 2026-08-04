package parser

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractJSData parses a JavaScript file, pulling out imports,
// functions (including arrow-function consts), classes, methods,
// exports, variable declarations, and within-file call links.
func extractJSData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractJSish(path, grammars.JavascriptLanguage(), extraNodeHandler{})
}

// extraNodeHandler lets a language in the JS family (e.g. TypeScript)
// extend the shared walker with declaration kinds that the base JS
// grammar does not know about. Both hooks are optional.
type extraNodeHandler struct {
	// declareSymbols runs during the symbol-registration pass so
	// language-specific declarations resolve like first-class names.
	declareSymbols func(w *jsishWalker, n *sitter.Node)
	// extractEntities runs during the extraction pass, before the
	// generic recursion, so language-specific entities are emitted
	// alongside the JS ones.
	extractEntities func(w *jsishWalker, n *sitter.Node)
}

// jsishWalker is the shared two-pass tree-sitter walker for the JS
// language family (JavaScript, TypeScript, TSX). It holds every piece
// of state the walk needs; language-specific behaviour plugs in through
// the extraNodeHandler.
type jsishWalker struct {
	content []byte
	path    string
	lang    *sitter.Language

	// declared maps every symbol to its entity ID so call sites and
	// export links resolve even when they appear earlier in the file.
	declared map[string]string
	// classStack names the enclosing class so methods are named
	// "ClassName.method" and stay unique across classes.
	classStack []string
	// currentFunc tracks the entity ID of the enclosing function or
	// method, so call_expression nodes know who the caller is.
	currentFunc []string

	entities []ir.Entity
	links    []ir.Link
	extra    extraNodeHandler
}

// extractJSish is the shared tree-sitter walker for the JS family.
// TypeScript-only declaration kinds are supplied via `extra`.
//
// The function uses a two-pass approach matching the Go extractor:
//   - Pass 1: register all function/class/method symbols so forward
//     references resolve correctly.
//   - Pass 2: extract entities, emit export links, and resolve
//     within-file call_expression nodes to "calls" links.
func extractJSish(path string, lang *sitter.Language, extra extraNodeHandler) ([]ir.Entity, []ir.Link, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read file: %w", err)
	}

	parser := sitter.NewParser(lang)

	tree, err := parser.Parse(content)
	if err != nil {
		return nil, nil, fmt.Errorf("tree-sitter failed parsing: %w", err)
	}

	w := &jsishWalker{
		content:  content,
		path:     path,
		lang:     lang,
		declared: make(map[string]string),
		entities: make([]ir.Entity, 0),
		links:    make([]ir.Link, 0),
		extra:    extra,
	}

	// Pass 1: register all declaration symbols.
	w.registerSymbols(tree.RootNode())
	// Pass 2: extract entities and resolve calls.
	w.inspectNode(tree.RootNode())
	return w.entities, w.links, nil
}

// registerSymbols walks the tree registering every function/method/class/
// variable symbol so forward references produce "calls" and "exports" links.
func (w *jsishWalker) registerSymbols(n *sitter.Node) {
	if n == nil {
		return
	}
	switch n.Type(w.lang) {
	case "function_declaration":
		if name := n.ChildByFieldName("name", w.lang); name != nil {
			funcName := strings.TrimSpace(string(w.content[name.StartByte():name.EndByte()]))
			if funcName != "" {
				w.declared[funcName] = fmt.Sprintf("%s#function:%s", w.path, funcName)
			}
		}
	case "class_declaration", "abstract_class_declaration":
		if name := n.ChildByFieldName("name", w.lang); name != nil {
			className := strings.TrimSpace(string(w.content[name.StartByte():name.EndByte()]))
			if className != "" {
				w.declared[className] = fmt.Sprintf("%s#class:%s", w.path, className)
			}
		}
	case "method_definition":
		if name := n.ChildByFieldName("name", w.lang); name != nil {
			methodName := strings.TrimSpace(string(w.content[name.StartByte():name.EndByte()]))
			if methodName != "" {
				w.declared[methodName] = fmt.Sprintf("%s#method:%s", w.path, methodName)
			}
		}
	case "variable_declarator":
		if name := n.ChildByFieldName("name", w.lang); name != nil {
			varName := strings.TrimSpace(string(w.content[name.StartByte():name.EndByte()]))
			if varName != "" {
				if value := n.ChildByFieldName("value", w.lang); value != nil {
					if value.Type(w.lang) == "arrow_function" || value.Type(w.lang) == "function" {
						w.declared[varName] = fmt.Sprintf("%s#function:%s", w.path, varName)
					}
				}
			}
		}
	}

	if w.extra.declareSymbols != nil {
		w.extra.declareSymbols(w, n)
	}

	for i := 0; i < int(n.ChildCount()); i++ {
		w.registerSymbols(n.Child(i))
	}
}

// inspectNode extracts entities and resolves within-file calls. Using a
// method keeps the shared walk state reachable without threading slices
// through function arguments at every recursion level.
func (w *jsishWalker) inspectNode(n *sitter.Node) {
	if n == nil {
		return
	}

	switch n.Type(w.lang) {
	case "import_statement":
		if source := n.ChildByFieldName("source", w.lang); source != nil {
			module := unquoteString(string(w.content[source.StartByte():source.EndByte()]))
			w.entities = append(w.entities, ir.Entity{
				ID:   fmt.Sprintf("%s#import:%s", w.path, module),
				Type: "import",
				Name: module,
				Metadata: map[string]string{
					"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
					"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
				},
			})
		}
		return

	case "export_statement":
		// Export declarations carry a "declaration" child (function,
		// class, or variable) that is extracted below via the generic
		// recursion.  We emit an "export" entity for the export itself
		// so the graph captures what a module exposes, then fall
		// through so the declaration is also visited.
		//
		// For "export default <identifier>" (no declaration child), we
		// extract the identifier name directly.
		if decl := n.ChildByFieldName("declaration", w.lang); decl != nil {
			var exportedName string
			switch decl.Type(w.lang) {
			case "function_declaration":
				if name := decl.ChildByFieldName("name", w.lang); name != nil {
					exportedName = strings.TrimSpace(string(w.content[name.StartByte():name.EndByte()]))
				}
			case "class_declaration", "abstract_class_declaration":
				if name := decl.ChildByFieldName("name", w.lang); name != nil {
					exportedName = strings.TrimSpace(string(w.content[name.StartByte():name.EndByte()]))
				}
			case "lexical_declaration", "variable_declaration":
				for i := 0; i < int(decl.ChildCount()); i++ {
					if decl.Child(i).Type(w.lang) == "variable_declarator" {
						vd := decl.Child(i)
						if name := vd.ChildByFieldName("name", w.lang); name != nil {
							exportedName = strings.TrimSpace(string(w.content[name.StartByte():name.EndByte()]))
							break
						}
					}
				}
			}
			if exportedName != "" {
				w.entities = append(w.entities, ir.Entity{
					ID:   fmt.Sprintf("%s#export:%s", w.path, exportedName),
					Type: "export",
					Name: exportedName,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
				if targetID, ok := w.declared[exportedName]; ok {
					w.links = append(w.links, ir.Link{
						SourceID:   fmt.Sprintf("%s#export:%s", w.path, exportedName),
						TargetID:   targetID,
						Type:       "exports",
						Weight:     1.0,
						SourceType: ir.LinkSourceExtracted,
					})
				}
			}
		} else {
			// "export default <identifier>" or "export { name, name2 }".
			// Walk children to find identifier names.
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type(w.lang) == "identifier" {
					exportedName := strings.TrimSpace(string(w.content[child.StartByte():child.EndByte()]))
					if exportedName == "" || exportedName == "default" {
						continue
					}
					w.entities = append(w.entities, ir.Entity{
						ID:   fmt.Sprintf("%s#export:%s", w.path, exportedName),
						Type: "export",
						Name: exportedName,
						Metadata: map[string]string{
							"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
							"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
						},
					})
					if targetID, ok := w.declared[exportedName]; ok {
						w.links = append(w.links, ir.Link{
							SourceID:   fmt.Sprintf("%s#export:%s", w.path, exportedName),
							TargetID:   targetID,
							Type:       "exports",
							Weight:     1.0,
							SourceType: ir.LinkSourceExtracted,
						})
					}
				}
			}
		}
		// Fall through to generic recursion so the declaration is visited.

	case "function_declaration":
		if name := n.ChildByFieldName("name", w.lang); name != nil {
			funcName := strings.TrimSpace(string(w.content[name.StartByte():name.EndByte()]))
			if funcName == "" {
				return
			}
			id := fmt.Sprintf("%s#function:%s", w.path, funcName)
			w.entities = append(w.entities, ir.Entity{
				ID:   id,
				Type: "function",
				Name: funcName,
				Metadata: map[string]string{
					"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
					"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
				},
			})
			w.currentFunc = append(w.currentFunc, id)
			for i := 0; i < int(n.ChildCount()); i++ {
				w.inspectNode(n.Child(i))
			}
			w.currentFunc = w.currentFunc[:len(w.currentFunc)-1]
		}
		return

	case "class_declaration", "abstract_class_declaration":
		if name := n.ChildByFieldName("name", w.lang); name != nil {
			className := strings.TrimSpace(string(w.content[name.StartByte():name.EndByte()]))
			if className == "" {
				return
			}
			w.entities = append(w.entities, ir.Entity{
				ID:   fmt.Sprintf("%s#class:%s", w.path, className),
				Type: "class",
				Name: className,
				Metadata: map[string]string{
					"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
					"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
				},
			})
			w.classStack = append(w.classStack, className)
			for i := 0; i < int(n.ChildCount()); i++ {
				w.inspectNode(n.Child(i))
			}
			w.classStack = w.classStack[:len(w.classStack)-1]
		}
		return

	case "method_definition":
		if name := n.ChildByFieldName("name", w.lang); name != nil {
			methodName := strings.TrimSpace(string(w.content[name.StartByte():name.EndByte()]))
			if methodName == "" {
				return
			}
			fullName := methodName
			if len(w.classStack) > 0 {
				fullName = w.classStack[len(w.classStack)-1] + "." + methodName
			}
			id := fmt.Sprintf("%s#method:%s", w.path, fullName)
			w.entities = append(w.entities, ir.Entity{
				ID:   id,
				Type: "method",
				Name: fullName,
				Metadata: map[string]string{
					"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
					"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
				},
			})
			w.currentFunc = append(w.currentFunc, id)
			for i := 0; i < int(n.ChildCount()); i++ {
				w.inspectNode(n.Child(i))
			}
			w.currentFunc = w.currentFunc[:len(w.currentFunc)-1]
		}
		return

	case "lexical_declaration", "variable_declaration":
		// Extract each variable declarator; arrow functions become
		// "function" entities, the rest become "variable" entities.
		for i := 0; i < int(n.ChildCount()); i++ {
			child := n.Child(i)
			if child.Type(w.lang) == "variable_declarator" {
				if nameNode := child.ChildByFieldName("name", w.lang); nameNode != nil {
					varName := strings.TrimSpace(string(w.content[nameNode.StartByte():nameNode.EndByte()]))
					if varName == "" {
						continue
					}
					value := child.ChildByFieldName("value", w.lang)
					if value != nil && value.Type(w.lang) == "arrow_function" {
						id := fmt.Sprintf("%s#function:%s", w.path, varName)
						w.entities = append(w.entities, ir.Entity{
							ID:   id,
							Type: "function",
							Name: varName,
							Metadata: map[string]string{
								"start_line": fmt.Sprintf("%d", child.StartPoint().Row+1),
								"end_line":   fmt.Sprintf("%d", child.EndPoint().Row+1),
							},
						})
						w.currentFunc = append(w.currentFunc, id)
						w.inspectNode(value)
						w.currentFunc = w.currentFunc[:len(w.currentFunc)-1]
					} else {
						w.entities = append(w.entities, ir.Entity{
							ID:   fmt.Sprintf("%s#variable:%s", w.path, varName),
							Type: "variable",
							Name: varName,
							Metadata: map[string]string{
								"start_line": fmt.Sprintf("%d", child.StartPoint().Row+1),
								"end_line":   fmt.Sprintf("%d", child.EndPoint().Row+1),
							},
						})
					}
				}
			}
		}
		return

	case "call_expression":
		// Simple identifier calls to locally-declared symbols
		// become "calls" links.  Member expression calls
		// (obj.method, pkg.fn) are left out of the within-file
		// graph for now.
		if fn := n.ChildByFieldName("function", w.lang); fn != nil && len(w.currentFunc) > 0 {
			var callee string
			switch fn.Type(w.lang) {
			case "identifier":
				callee = string(w.content[fn.StartByte():fn.EndByte()])
			}
			if callee != "" {
				if target, ok := w.declared[callee]; ok {
					w.links = append(w.links, ir.Link{
						SourceID:   w.currentFunc[len(w.currentFunc)-1],
						TargetID:   target,
						Type:       "calls",
						Weight:     1.0,
						SourceType: ir.LinkSourceExtracted,
					})
				}
			}
		}
		// Fall through so nested call expressions are visited.
	}

	// Language-specific declaration kinds (e.g. TypeScript interfaces,
	// type aliases, and enums) are emitted before the generic recursion.
	if w.extra.extractEntities != nil {
		w.extra.extractEntities(w, n)
	}

	for i := 0; i < int(n.ChildCount()); i++ {
		w.inspectNode(n.Child(i))
	}
}

// unquoteString strips surrounding single or double quotes from a
// string literal, resolving escapes where possible.
func unquoteString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if unquoted, err := strconv.Unquote(s); err == nil {
			return unquoted
		}
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "\\'", "'")
	}
	return s
}
