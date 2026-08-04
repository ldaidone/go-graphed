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
// functions (including arrow-function consts), classes, and methods.
func extractJSData(path string) ([]ir.Entity, error) {
	return extractJSish(path, grammars.JavascriptLanguage(), false)
}

// extractTSData parses a TypeScript file, adding the type-level
// declarations (interfaces, type aliases, enums) on top of the JS set.
func extractTSData(path string) ([]ir.Entity, error) {
	return extractJSish(path, grammars.TypescriptLanguage(), true)
}

// extractTSXData parses a TSX file with the TypeScript rule set.
func extractTSXData(path string) ([]ir.Entity, error) {
	return extractJSish(path, grammars.TsxLanguage(), true)
}

// extractJSish is the shared tree-sitter walker for the JS family.
// isTS enables the TypeScript-only declaration kinds.
func extractJSish(path string, lang *sitter.Language, isTS bool) ([]ir.Entity, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}

	parser := sitter.NewParser(lang)

	tree, err := parser.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter failed parsing: %w", err)
	}

	var entities []ir.Entity

	// classStack names the enclosing class so methods are named
	// "ClassName.method" and stay unique across classes.
	var classStack []string

	var inspectNode func(*sitter.Node)
	inspectNode = func(n *sitter.Node) {
		if n == nil {
			return
		}

		switch n.Type(lang) {
		case "import_statement":
			if source := n.ChildByFieldName("source", lang); source != nil {
				module := unquoteString(string(content[source.StartByte():source.EndByte()]))
				entities = append(entities, ir.Entity{
					ID:   fmt.Sprintf("%s#import:%s", path, module),
					Type: "import",
					Name: module,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
			}
			return

		case "function_declaration":
			if name := n.ChildByFieldName("name", lang); name != nil {
				entities = append(entities, ir.Entity{
					ID:   fmt.Sprintf("%s#function:%s", path, string(content[name.StartByte():name.EndByte()])),
					Type: "function",
					Name: string(content[name.StartByte():name.EndByte()]),
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
			}
			return

		case "class_declaration", "abstract_class_declaration":
			if name := n.ChildByFieldName("name", lang); name != nil {
				className := string(content[name.StartByte():name.EndByte()])
				entities = append(entities, ir.Entity{
					ID:   fmt.Sprintf("%s#class:%s", path, className),
					Type: "class",
					Name: className,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
				classStack = append(classStack, className)
				for i := 0; i < int(n.ChildCount()); i++ {
					inspectNode(n.Child(i))
				}
				classStack = classStack[:len(classStack)-1]
			}
			return

		case "method_definition":
			if name := n.ChildByFieldName("name", lang); name != nil {
				methodName := string(content[name.StartByte():name.EndByte()])
				fullName := methodName
				if len(classStack) > 0 {
					fullName = classStack[len(classStack)-1] + "." + methodName
				}
				entities = append(entities, ir.Entity{
					ID:   fmt.Sprintf("%s#method:%s", path, fullName),
					Type: "method",
					Name: fullName,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				})
			}
			return

		case "variable_declarator":
			if name := n.ChildByFieldName("name", lang); name != nil {
				if value := n.ChildByFieldName("value", lang); value != nil && value.Type(lang) == "arrow_function" {
					funcName := string(content[name.StartByte():name.EndByte()])
					entities = append(entities, ir.Entity{
						ID:   fmt.Sprintf("%s#function:%s", path, funcName),
						Type: "function",
						Name: funcName,
						Metadata: map[string]string{
							"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
							"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
						},
					})
				}
			}
			return
		}

		if isTS {
			switch n.Type(lang) {
			case "interface_declaration", "type_alias_declaration", "enum_declaration":
				kind := "type-alias"
				switch n.Type(lang) {
				case "interface_declaration":
					kind = "interface"
				case "enum_declaration":
					kind = "enum"
				}
				if name := n.ChildByFieldName("name", lang); name != nil {
					declName := string(content[name.StartByte():name.EndByte()])
					entities = append(entities, ir.Entity{
						ID:   fmt.Sprintf("%s#%s:%s", path, kind, declName),
						Type: kind,
						Name: declName,
						Metadata: map[string]string{
							"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
							"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
						},
					})
				}
				// Fall through to generic recursion so body members are
				// still visited.
			}
		}

		for i := 0; i < int(n.ChildCount()); i++ {
			inspectNode(n.Child(i))
		}
	}

	inspectNode(tree.RootNode())
	return entities, nil
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
