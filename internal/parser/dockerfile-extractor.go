package parser

import (
	"fmt"
	"os"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractDockerfileData uses tree-sitter to parse a Dockerfile and
// pull out each top-level instruction. FROM lines additionally record
// the base image so downstream tools can spot dependency roots.
func extractDockerfileData(path string) ([]ir.Entity, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}

	lang := grammars.DockerfileLanguage()
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

		// Instruction node types carry a "_instruction" suffix, e.g.
		// "from_instruction", "run_instruction", "copy_instruction".
		if before, ok := strings.CutSuffix(n.Type(lang), "_instruction"); ok {
			keyword := strings.ToUpper(before)
			entity := ir.Entity{
				ID:   fmt.Sprintf("%s#%s", path, keyword),
				Type: "instruction",
				Name: keyword,
				Metadata: map[string]string{
					"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
					"end_line":   fmt.Sprintf("%d", n.EndPoint().Row+1),
				},
			}
			// FROM <image> registers the base image as a dependency.
			if keyword == "FROM" {
				for i := 0; i < int(n.ChildCount()); i++ {
					if n.Child(i).Type(lang) == "image_spec" {
						entity.Metadata["base_image"] = strings.TrimSpace(string(content[n.Child(i).StartByte():n.Child(i).EndByte()]))
						break
					}
				}
			}
			entities = append(entities, entity)
			return
		}

		for i := 0; i < int(n.ChildCount()); i++ {
			inspectNode(n.Child(i))
		}
	}

	inspectNode(tree.RootNode())
	return entities, nil
}
