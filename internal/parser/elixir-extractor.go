package parser

import (
	"fmt"
	"os"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractElixirData parses an Elixir file, pulling out modules, def/
// defp functions, alias/import/require/use directives, and within-file
// call links.
func extractElixirData(path string) ([]ir.Entity, []ir.Link, error) {
	var err error
	var content []byte
	var tree *sitter.Tree

	content, err = os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read file: %w", err)
	}
	p := sitter.NewParser(grammars.ElixirLanguage())
	tree, err = p.Parse(content)
	if err != nil {
		return nil, nil, fmt.Errorf("tree-sitter failed parsing: %w", err)
	}
	e := &elixirExtractor{
		content:  content,
		path:     path,
		lang:     grammars.ElixirLanguage(),
		declared: make(map[string]string),
		entities: []ir.Entity{},
		links:    []ir.Link{},
	}
	e.register(tree.RootNode())
	e.inspect(tree.RootNode())
	return e.entities, e.links, nil
}

// elixirExtractor is a dedicated two-pass extractor.  Elixir's grammar
// is expression-based: defmodule, def, alias, import, and plain calls
// are all "call" nodes distinguished only by their leading identifier,
// which cannot be expressed as LanguageConfig table rows, so the passes
// special-case them by keyword.
type elixirExtractor struct {
	content []byte
	path    string
	lang    *sitter.Language

	declared    map[string]string
	scopeStack  []string
	currentFunc []string

	entities []ir.Entity
	links    []ir.Link
}

// defKeywords are the function-defining call targets (plus their private
// and macro forms) handled as declarations.
var defKeywords = map[string]bool{
	"def": true, "defp": true, "defmacro": true, "defmacrop": true,
	"defguard": true, "defguardp": true, "defexception": true, "defstruct": true,
}

// importKeywords are the module-pulling call targets turned into import
// entities.
var importKeywords = map[string]bool{
	"alias": true, "import": true, "require": true, "use": true,
}

// callTarget returns the trimmed text of a call node's target field.
func (e *elixirExtractor) callTarget(n *sitter.Node) string {
	t := n.ChildByFieldName("target", e.lang)
	if t == nil {
		return ""
	}
	return strings.TrimSpace(string(e.content[t.StartByte():t.EndByte()]))
}

// childByType returns the first direct child of the given type.  The
// embedded Elixir grammar only assigns the "target" field, so arguments/
// do_block children must be located by node type.
func (e *elixirExtractor) childByType(n *sitter.Node, nodeType string) *sitter.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		if c := n.Child(i); c.Type(e.lang) == nodeType {
			return c
		}
	}
	return nil
}

// firstDescendant returns the first descendant node of the given type.
func (e *elixirExtractor) firstDescendant(n *sitter.Node, nodeType string) *sitter.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child.Type(e.lang) == nodeType {
			return child
		}
		if found := e.firstDescendant(child, nodeType); found != nil {
			return found
		}
	}
	return nil
}

// firstAlias returns the trimmed text of the first alias node under n
// (used for module names in defmodule and alias/import/require/use).
func (e *elixirExtractor) firstAlias(n *sitter.Node) string {
	if a := e.firstDescendant(n, "alias"); a != nil {
		return strings.TrimSpace(string(e.content[a.StartByte():a.EndByte()]))
	}
	return ""
}

// defHeadName resolves the function name of a def/defp/... call: the
// target of the head call in its arguments, falling back to a bare
// identifier/alias argument.
func (e *elixirExtractor) defHeadName(n *sitter.Node) string {
	args := e.childByType(n, "arguments")
	if args == nil {
		return ""
	}
	if head := e.firstDescendant(args, "call"); head != nil {
		if name := e.callTarget(head); name != "" {
			return name
		}
	}
	for _, t := range []string{"identifier", "alias"} {
		if nd := e.firstDescendant(args, t); nd != nil {
			return strings.TrimSpace(string(e.content[nd.StartByte():nd.EndByte()]))
		}
	}
	return ""
}

// register is pass 1: record module and function symbols so forward
// references resolve.
func (e *elixirExtractor) register(n *sitter.Node) {
	if n.Type(e.lang) == "call" {
		switch e.callTarget(n) {
		case "defmodule":
			if name := e.firstAlias(n); name != "" {
				e.declared[name] = fmt.Sprintf("%s#module:%s", e.path, name)
				e.scopeStack = append(e.scopeStack, name)
			}
		default:
			if defKeywords[e.callTarget(n)] {
				if name := e.defHeadName(n); name != "" {
					full := e.qualify(name)
					e.declared[full] = fmt.Sprintf("%s#function:%s", e.path, full)
				}
			}
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		e.register(n.Child(i))
	}
	if n.Type(e.lang) == "call" && e.callTarget(n) == "defmodule" && len(e.scopeStack) > 0 {
		e.scopeStack = e.scopeStack[:len(e.scopeStack)-1]
	}
}

// qualify prefixes a bare name with the innermost module scope.
func (e *elixirExtractor) qualify(name string) string {
	if len(e.scopeStack) > 0 {
		return e.scopeStack[len(e.scopeStack)-1] + "." + name
	}
	return name
}

// inspect is pass 2: emit entities, imports, and calls links.
func (e *elixirExtractor) inspect(n *sitter.Node) {
	if n.Type(e.lang) != "call" {
		for i := 0; i < int(n.ChildCount()); i++ {
			e.inspect(n.Child(i))
		}
		return
	}

	target := e.callTarget(n)
	switch {
	case target == "defmodule":
		name := e.firstAlias(n)
		if name == "" {
			return
		}
		e.entities = append(e.entities, ir.Entity{
			ID:       fmt.Sprintf("%s#module:%s", e.path, name),
			Type:     "module",
			Name:     name,
			Metadata: lineMetadata(n),
		})
		// Top-level modules double as the file's package: the first
		// dotted segment ("Geometry" from "Geometry.Shapes") is the
		// aggregation key the analyzer groups modules by.
		if len(e.scopeStack) == 0 {
			pkg := name
			if before, _, ok := strings.Cut(name, "."); ok {
				pkg = before
			}
			e.entities = append(e.entities, ir.Entity{
				ID:   fmt.Sprintf("%s#package:%s", e.path, pkg),
				Type: "package",
				Name: pkg,
				Metadata: map[string]string{
					"start_line":   lineMetadata(n)["start_line"],
					"end_line":     lineMetadata(n)["end_line"],
					"package_path": pkg,
				},
			})
		}
		e.scopeStack = append(e.scopeStack, name)
		if body := e.childByType(n, "do_block"); body != nil {
			e.inspect(body)
		}
		e.scopeStack = e.scopeStack[:len(e.scopeStack)-1]

	case defKeywords[target]:
		name := e.defHeadName(n)
		if name == "" {
			return
		}
		full := e.qualify(name)
		id := fmt.Sprintf("%s#function:%s", e.path, full)
		e.entities = append(e.entities, ir.Entity{
			ID:       id,
			Type:     "function",
			Name:     full,
			Metadata: lineMetadata(n),
		})
		e.currentFunc = append(e.currentFunc, id)
		if body := e.childByType(n, "do_block"); body != nil {
			e.inspect(body)
		}
		e.currentFunc = e.currentFunc[:len(e.currentFunc)-1]

	case importKeywords[target]:
		e.emitImport(n)
		for i := 0; i < int(n.ChildCount()); i++ {
			e.inspect(n.Child(i))
		}

	default:
		e.emitCall(n, target)
		for i := 0; i < int(n.ChildCount()); i++ {
			e.inspect(n.Child(i))
		}
	}
}

// emitImport registers one alias/import/require/use directive as an
// import entity named after its first alias argument.
func (e *elixirExtractor) emitImport(n *sitter.Node) {
	args := e.childByType(n, "arguments")
	if args == nil {
		return
	}
	module := e.firstAlias(args)
	if module == "" {
		return
	}
	e.entities = append(e.entities, ir.Entity{
		ID:       fmt.Sprintf("%s#import:%s", e.path, module),
		Type:     "import",
		Name:     module,
		Metadata: lineMetadata(n),
	})
}

// emitCall resolves a bare call against the file's declared symbols,
// falling back to the enclosing module scope for "helper()" → "Mod.helper".
// Member and fully-qualified calls (Foo.bar, x.bar) carry dotted target
// text and never match.
func (e *elixirExtractor) emitCall(n *sitter.Node, target string) {
	if len(e.currentFunc) == 0 || target == "" {
		return
	}
	if strings.Contains(target, ".") {
		return
	}
	trg, ok := e.declared[target]
	if !ok {
		trg, ok = e.declared[e.qualify(target)]
	}
	if !ok {
		return
	}
	e.links = append(e.links, ir.Link{
		SourceID:   e.currentFunc[len(e.currentFunc)-1],
		TargetID:   trg,
		Type:       "calls",
		Weight:     1.0,
		SourceType: ir.LinkSourceExtracted,
	})
}
