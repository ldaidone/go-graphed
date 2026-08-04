package parser

import (
	"fmt"
	"os"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractYAMLData uses tree-sitter to parse YAML and pull out the
// mapping keys and sequences that give a YAML document its shape.
// Keys are the semantic units (config files, CI pipelines, manifests),
// so each mapping pair becomes a property/container entity and scalar
// sequence items become sequence-item entities.
func extractYAMLData(path string) ([]ir.Entity, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}

	lang := grammars.YamlLanguage()
	parser := sitter.NewParser(lang)

	tree, err := parser.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter failed parsing: %w", err)
	}

	var entities []ir.Entity

	// keyPath tracks the chain of enclosing mapping keys so entity
	// IDs stay unique across nested documents.
	var keyPath []string
	// seqStack holds a running item index per enclosing block_sequence,
	// used to name container-valued sequence items uniquely.
	var seqStack []int

	var inspectNode func(*sitter.Node)
	inspectNode = func(n *sitter.Node) {
		if n == nil {
			return
		}

		switch n.Type(lang) {
		case "block_mapping_pair":
			keyNode := pairKeyNode(lang, n)
			valueNode := pairValueNode(lang, n)
			if keyNode != nil {
				keyName := unquoteYAMLScalar(string(content[keyNode.StartByte():keyNode.EndByte()]))
				fullName := keyName
				if len(keyPath) > 0 {
					fullName = strings.Join(append(append([]string{}, keyPath...), keyName), ".")
				}

				keyPath = append(keyPath, keyName)

				if container := containerKind(lang, valueNode); container != "" {
					// Mapping/sequence values become a single entity of
					// the container kind, named after the key path.
					entities = append(entities, ir.Entity{
						ID:   fmt.Sprintf("%s#%s:%s", path, container, fullName),
						Type: container,
						Name: fullName,
						Metadata: map[string]string{
							"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
							"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
						},
					})
					if valueNode != nil {
						inspectNode(valueNode)
					}
				} else {
					entities = append(entities, ir.Entity{
						ID:   fmt.Sprintf("%s#property:%s", path, fullName),
						Type: "property",
						Name: fullName,
						Metadata: map[string]string{
							"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
							"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
						},
					})
				}

				keyPath = keyPath[:len(keyPath)-1]
			}
			return

		case "block_sequence":
			seqStack = append(seqStack, 0)
			for i := 0; i < int(n.ChildCount()); i++ {
				inspectNode(n.Child(i))
			}
			seqStack = seqStack[:len(seqStack)-1]
			return

		case "block_sequence_item":
			if len(seqStack) == 0 {
				return
			}
			idx := seqStack[len(seqStack)-1]
			seqStack[len(seqStack)-1]++
			valueNode := itemValueNode(lang, n)
			if container := containerKind(lang, valueNode); container != "" {
				// Container-valued items get an indexed key so their
				// inner pairs stay unique (e.g. "list.0.name").
				keyPath = append(keyPath, fmt.Sprintf("%d", idx))
				inspectNode(valueNode)
				keyPath = keyPath[:len(keyPath)-1]
			} else if valueNode != nil {
				entities = append(entities, ir.Entity{
					ID:   fmt.Sprintf("%s#sequence-item:%d", path, idx),
					Type: "sequence-item",
					Name: unquoteYAMLScalar(string(content[valueNode.StartByte():valueNode.EndByte()])),
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", valueNode.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", valueNode.EndPoint().Row+1),
					},
				})
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

// pairKeyNode returns the key node of a block_mapping_pair (a
// flow_node wrapping a scalar in the tree-sitter-yaml grammar).
func pairKeyNode(lang *sitter.Language, n *sitter.Node) *sitter.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child.Type(lang) == "flow_node" {
			return child
		}
	}
	return nil
}

// pairValueNode returns the value node of a block_mapping_pair, which
// is either a flow_node (scalar) or a block_node (container).
func pairValueNode(lang *sitter.Language, n *sitter.Node) *sitter.Node {
	for i := int(n.ChildCount()) - 1; i >= 0; i-- {
		child := n.Child(i)
		t := child.Type(lang)
		if t == "flow_node" || t == "block_node" {
			return child
		}
	}
	return nil
}

// itemValueNode returns the value node of a block_sequence_item.
func itemValueNode(lang *sitter.Language, n *sitter.Node) *sitter.Node {
	return pairValueNode(lang, n)
}

// containerKind classifies a YAML value node as "object", "array", or
// "" when it holds a scalar. block_node/flow_node wrap the actual
// mapping/sequence node.
func containerKind(lang *sitter.Language, n *sitter.Node) string {
	if n == nil {
		return ""
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		switch n.Child(i).Type(lang) {
		case "block_mapping", "flow_mapping":
			return "object"
		case "block_sequence", "flow_sequence":
			return "array"
		}
	}
	return ""
}

// unquoteYAMLScalar strips the surrounding quotes from a scalar and
// trims it. Nested-quote/escape handling is intentionally basic.
func unquoteYAMLScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
