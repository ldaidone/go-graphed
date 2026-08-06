package parser

import (
	"fmt"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractRubyData parses a Ruby file, pulling out modules, classes,
// methods, requires, and within-file call links.
func extractRubyData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, rubyConfig())
}

// rubyConfig maps the tree-sitter-ruby grammar onto the shared walker.
// Notes on the grammar:
//
//   - module and class declarations carry their name in the "name"
//     field (a constant) and act as scopes, so methods get
//     "Module::Class.method" style names.
//   - method (def) and singleton_method (def self.x) declarations use
//     the "name" field; MethodInClass renames class-scoped ones.
//   - require/require_relative/load are ordinary "call" nodes, so they
//     cannot be an ImportSpec row; rubyInspectHook emits them.
//   - call nodes use the "method" field for the called name.  Calls with
//     a receiver (obj.helper, self.helper, Foo.new) are skipped via
//     ObjectField, so only bare calls resolve -- and bare no-parenthesis
//     calls are parsed as plain identifiers by tree-sitter-ruby, so only
//     parenthesized/argumented calls produce links.
func rubyConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.RubyLanguage(),
		Declarations: []DeclarationSpec{
			{
				NodeType:   "module",
				Kind:       "module",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:   "class",
				Kind:       "class",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:      "method",
				Kind:          "function",
				NameField:     "name",
				FunctionScope: true,
				MethodInClass: true,
			},
			{
				NodeType:      "singleton_method",
				Kind:          "function",
				NameField:     "name",
				FunctionScope: true,
				MethodInClass: true,
			},
		},
		Calls: []CallSpec{
			{NodeType: "call", FunctionField: "method", ObjectField: "receiver"},
		},
		InspectHook: rubyInspectHook,
	}
}

// rubyInspectHook emits "import" entities for require/require_relative
// and load calls, reading the module name from the first string
// argument.
func rubyInspectHook(w *Walker, n *sitter.Node) {
	if n.Type(w.lang()) != "call" {
		return
	}
	kw := ""
	if f := n.ChildByFieldName("method", w.lang()); f != nil {
		kw = strings.TrimSpace(string(w.content[f.StartByte():f.EndByte()]))
	}
	if kw != "require" && kw != "require_relative" && kw != "load" {
		return
	}
	module := ""
	if args := n.ChildByFieldName("arguments", w.lang()); args != nil {
		if s := w.childOfType(args, "string"); s != nil {
			if c := w.childOfType(s, "string_content"); c != nil {
				module = strings.TrimSpace(string(w.content[c.StartByte():c.EndByte()]))
			}
		}
	}
	if module == "" {
		return
	}
	w.entities = append(w.entities, ir.Entity{
		ID:       fmt.Sprintf("%s#import:%s", w.path, module),
		Type:     "import",
		Name:     module,
		Metadata: lineMetadata(n),
	})
}
