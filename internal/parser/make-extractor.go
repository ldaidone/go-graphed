package parser

import (
	"fmt"
	"os"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractMakefileData uses tree-sitter to parse a Makefile and pull
// out the build targets and variable assignments that define it.
func extractMakefileData(path string) ([]ir.Entity, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}

	lang := grammars.MakeLanguage()
	parser := sitter.NewParser(lang)

	tree, err := parser.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter failed parsing: %w", err)
	}

	var entities []ir.Entity

	var inspectNode func(*sitter.Node)
	inspectNode = func(n *sitter.Node) {
		if n == nil {
			return
		}

		switch n.Type(lang) {
		case "rule":
			var targets []*sitter.Node
			var prereqs []string
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				switch child.Type(lang) {
				case "targets":
					for j := 0; j < int(child.ChildCount()); j++ {
						if child.Child(j).Type(lang) == "word" {
							targets = append(targets, child.Child(j))
						}
					}
				case "prerequisites":
					for j := 0; j < int(child.ChildCount()); j++ {
						if child.Child(j).Type(lang) == "word" {
							prereqs = append(prereqs, strings.TrimSpace(string(content[child.Child(j).StartByte():child.Child(j).EndByte()])))
						}
					}
				}
			}
			for _, t := range targets {
				name := strings.TrimSpace(string(content[t.StartByte():t.EndByte()]))
				// Directives like ".PHONY" are metadata, not targets.
				if strings.HasPrefix(name, ".") {
					continue
				}
				// Under error recovery a rule may surface with an empty
				// target (e.g. a lone ":"); skip it rather than emitting a
				// blank-named entity.
				if name == "" {
					continue
				}
				entity := ir.Entity{
					ID:   fmt.Sprintf("%s#target:%s", path, name),
					Type: "target",
					Name: name,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				}
				if len(prereqs) > 0 {
					entity.Metadata["prerequisites"] = strings.Join(prereqs, ", ")
				}
				entities = append(entities, entity)
			}
			return

		case "variable_assignment":
			var nameNode, valueNode *sitter.Node
			for i := 0; i < int(n.ChildCount()); i++ {
				switch n.Child(i).Type(lang) {
				case "word":
					if nameNode == nil {
						nameNode = n.Child(i)
					}
				case "text":
					if valueNode == nil {
						valueNode = n.Child(i)
					}
				}
			}
			if nameNode != nil {
				name := strings.TrimSpace(string(content[nameNode.StartByte():nameNode.EndByte()]))
				// Under error recovery a variable may surface without a name;
				// skip it rather than emitting a blank-named entity.
				if name == "" {
					return
				}
				entity := ir.Entity{
					ID:   fmt.Sprintf("%s#variable:%s", path, name),
					Type: "variable",
					Name: name,
					Metadata: map[string]string{
						"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
						"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
					},
				}
				if valueNode != nil {
					entity.Metadata["value"] = strings.TrimSpace(string(content[valueNode.StartByte():valueNode.EndByte()]))
				}
				entities = append(entities, entity)
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
