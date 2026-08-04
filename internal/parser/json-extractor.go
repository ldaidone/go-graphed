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

// extractJSONData uses tree-sitter to parse JSON and pull out the
// object keys and nested containers that make up its structure.
// Keys are the semantic units of a JSON document (package.json,
// tsconfig.json, config files), so each "pair" becomes a property
// entity and nested objects/arrays become container entities.
func extractJSONData(path string) ([]ir.Entity, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}

	lang := grammars.JsonLanguage()
	parser := sitter.NewParser(lang)

	tree, err := parser.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter failed parsing: %w", err)
	}

	var entities []ir.Entity

	// keyPath tracks the chain of enclosing keys so entity IDs stay
	// unique even when the same key name appears in nested objects.
	var keyPath []string

	var inspectNode func(*sitter.Node)
	inspectNode = func(n *sitter.Node) {
		if n == nil {
			return
		}

		switch n.Type(lang) {
		case "pair":
			key := n.ChildByFieldName("key", lang)
			val := n.ChildByFieldName("value", lang)
			if key != nil {
				keyName := unquoteJSONKey(string(content[key.StartByte():key.EndByte()]))
				fullName := keyName
				if len(keyPath) > 0 {
					fullName = strings.Join(append(append([]string{}, keyPath...), keyName), ".")
				}

				keyPath = append(keyPath, keyName)

				if val != nil && (val.Type(lang) == "object" || val.Type(lang) == "array") {
					// Container-valued pairs become a single entity of
					// the container kind, named after the key path.
					entities = append(entities, ir.Entity{
						ID:   fmt.Sprintf("%s#%s:%s", path, val.Type(lang), fullName),
						Type: val.Type(lang),
						Name: fullName,
						Metadata: map[string]string{
							"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
							"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
						},
					})
					inspectNode(val)
				} else {
					// Scalar-valued pairs are plain property entities.
					entities = append(entities, ir.Entity{
						ID:   fmt.Sprintf("%s#property:%s", path, fullName),
						Type: "property",
						Name: fullName,
						Metadata: map[string]string{
							"start_line": fmt.Sprintf("%d", key.StartPoint().Row+1),
							"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
						},
					})
				}

				keyPath = keyPath[:len(keyPath)-1]
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

// unquoteJSONKey strips the surrounding quotes from a JSON key and
// resolves escape sequences. Keys that are not quoted (syntax errors)
// are returned trimmed and unchanged.
func unquoteJSONKey(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if unquoted, err := strconv.Unquote(s); err == nil {
			return unquoted
		}
	}
	return s
}
