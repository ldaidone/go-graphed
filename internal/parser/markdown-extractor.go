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

// extractMarkdownData parses a Markdown document with tree-sitter and pulls
// out structural entities: headings, links, reference definitions, and
// images. Tree-sitter is used for consistency with extractGoData and because
// the grammar handles Markdown structure correctly: content inside fenced
// code blocks and HTML blocks is never misread as links, and reference/shortcut
// link forms are recognized alongside inline links.
//
// The tree-sitter-markdown grammar is split in two: the block grammar parses
// headings, paragraphs, code fences, lists and reference definitions; inline
// content is opaque text that the separate markdown_inline grammar parses
// into links and images. Both are embedded in gotreesitter.
func extractMarkdownData(path string) ([]ir.Entity, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}

	lang := grammars.MarkdownLanguage()
	parser := sitter.NewParser(lang)

	tree, err := parser.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter failed parsing markdown: %w", err)
	}

	inlineLang := grammars.MarkdownInlineLanguage()
	inlineParser := sitter.NewParser(inlineLang)

	var entities []ir.Entity
	headingIdx := 0
	linkIdx := 0

	// extractInline pulls links and images out of one opaque inline block.
	// The inline grammar's root node is itself named "inline", so walk its
	// children looking for the link/image node kinds.
	extractInline := func(text string, baseRow int, baseByte uint32) {
		inlineTree, err := inlineParser.Parse([]byte(text))
		if err != nil {
			return
		}

		// Line of an inline child = baseRow (row where the inline block
		// starts) plus any newlines preceding the child within the block.
		lineOf := func(n *sitter.Node) int {
			return baseRow + strings.Count(text[:n.StartByte()], "\n")
		}

		var walk func(*sitter.Node)
		walk = func(n *sitter.Node) {
			if n == nil {
				return
			}

			switch n.Type(inlineLang) {
			case "inline_link":
				linkIdx++
				entities = append(entities, ir.Entity{
					ID:   fmt.Sprintf("%s#link-%d", path, linkIdx),
					Type: "link",
					Name: nodeText([]byte(text), childOfType(n, "link_text", inlineLang)),
					Metadata: map[string]string{
						"target":     nodeText([]byte(text), childOfType(n, "link_destination", inlineLang)),
						"start_line": fmt.Sprintf("%d", lineOf(n)),
					},
				})

			case "full_reference_link":
				linkIdx++
				entities = append(entities, ir.Entity{
					ID:   fmt.Sprintf("%s#link-%d", path, linkIdx),
					Type: "link",
					Name: nodeText([]byte(text), childOfType(n, "link_text", inlineLang)),
					Metadata: map[string]string{
						"target":     trimLinkLabel(nodeText([]byte(text), childOfType(n, "link_label", inlineLang))),
						"start_line": fmt.Sprintf("%d", lineOf(n)),
					},
				})

			case "shortcut_link":
				linkIdx++
				entities = append(entities, ir.Entity{
					ID:   fmt.Sprintf("%s#link-%d", path, linkIdx),
					Type: "link",
					Name: nodeText([]byte(text), childOfType(n, "link_text", inlineLang)),
					Metadata: map[string]string{
						"target":     nodeText([]byte(text), childOfType(n, "link_text", inlineLang)),
						"start_line": fmt.Sprintf("%d", lineOf(n)),
					},
				})

			case "image":
				linkIdx++
				entities = append(entities, ir.Entity{
					ID:   fmt.Sprintf("%s#image-%d", path, linkIdx),
					Type: "image-asset",
					Name: nodeText([]byte(text), childOfType(n, "image_description", inlineLang)),
					Metadata: map[string]string{
						"target":     nodeText([]byte(text), childOfType(n, "link_destination", inlineLang)),
						"start_line": fmt.Sprintf("%d", lineOf(n)),
					},
				})
			}

			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i))
			}
		}
		walk(inlineTree.RootNode())
	}

	// Recursive AST walker over the block grammar. Code-fence and HTML-block
	// content never appears as an "inline" node, so links inside them are
	// naturally skipped.
	var inspect func(*sitter.Node)
	inspect = func(n *sitter.Node) {
		if n == nil {
			return
		}

		switch n.Type(lang) {
		case "atx_heading":
			headingIdx++
			entities = append(entities, ir.Entity{
				ID:   fmt.Sprintf("%s#heading-%d", path, headingIdx),
				Type: "heading",
				Name: headingTitle(content, n, lang),
				Metadata: map[string]string{
					"level":      atxHeadingLevel(lang, n),
					"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
				},
			})

		case "setext_heading":
			headingIdx++
			entities = append(entities, ir.Entity{
				ID:   fmt.Sprintf("%s#heading-%d", path, headingIdx),
				Type: "heading",
				Name: headingTitle(content, n, lang),
				Metadata: map[string]string{
					"level":      setextHeadingLevel(lang, n),
					"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
				},
			})

		case "link_reference_definition":
			linkIdx++
			entities = append(entities, ir.Entity{
				ID:   fmt.Sprintf("%s#ref-%d", path, linkIdx),
				Type: "link",
				Name: trimLinkLabel(nodeText(content, childOfType(n, "link_label", lang))),
				Metadata: map[string]string{
					"target":     nodeText(content, childOfType(n, "link_destination", lang)),
					"start_line": fmt.Sprintf("%d", n.StartPoint().Row+1),
				},
			})

		case "inline":
			extractInline(nodeText(content, n), int(n.StartPoint().Row), n.StartByte())
		}

		for i := 0; i < int(n.ChildCount()); i++ {
			inspect(n.Child(i))
		}
	}

	inspect(tree.RootNode())
	return entities, nil
}

// nodeText returns the source text spanned by n, or "" when n is nil.
func nodeText(content []byte, n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return string(content[n.StartByte():n.EndByte()])
}

// childOfType returns the first direct child of n with the given type.
func childOfType(n *sitter.Node, t string, lang *sitter.Language) *sitter.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if c := n.Child(i); c.Type(lang) == t {
			return c
		}
	}
	return nil
}

// headingTitle is the text of a heading's inline block, trimmed. ATX
// headings carry the inline node directly; setext headings wrap it in a
// paragraph node.
func headingTitle(content []byte, n *sitter.Node, lang *sitter.Language) string {
	if p := childOfType(n, "paragraph", lang); p != nil {
		return strings.TrimSpace(nodeText(content, childOfType(p, "inline", lang)))
	}
	return strings.TrimSpace(nodeText(content, childOfType(n, "inline", lang)))
}

// atxHeadingLevel maps an atx_hN_marker child to its heading depth.
func atxHeadingLevel(lang *sitter.Language, n *sitter.Node) string {
	for i := 1; i <= 6; i++ {
		if childOfType(n, fmt.Sprintf("atx_h%d_marker", i), lang) != nil {
			return strconv.Itoa(i)
		}
	}
	return "1"
}

// setextHeadingLevel reads the depth from a setext underline child.
func setextHeadingLevel(lang *sitter.Language, n *sitter.Node) string {
	if childOfType(n, "setext_h1_underline", lang) != nil {
		return "1"
	}
	return "2"
}

// trimLinkLabel strips the surrounding brackets from a link label such as
// "[ref]" and returns the bare label "ref".
func trimLinkLabel(s string) string {
	return strings.Trim(s, "[]")
}
