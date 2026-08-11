package parser

import (
	"fmt"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractPHPData parses a PHP file, pulling out namespace imports,
// classes, interfaces, traits, methods, top-level functions, and
// within-file call links.
func extractPHPData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, phpConfig())
}

// phpConfig maps the tree-sitter-php grammar onto the shared walker.
// Notes on the grammar:
//
//   - classes, interfaces, and traits declare a name field and act as
//     scopes, so methods get "Class.method" names.
//   - method_declaration covers class methods; function_definition
//     covers top-level functions.
//   - namespace_use_declaration imports cannot be expressed as an
//     ImportSpec row (the module path lives in a namespace_use_clause
//     child with an optional "as" alias), so phpInspectHook emits them.
//   - Calls: function_call_expression for "helper()" (resolves via the
//     class-scoped fallback inside a class) and object_creation_expression
//     for "new Service()" (callee is a name/qualified_name child).
//     $this->member() calls are skipped.
func phpConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.PhpLanguage(),
		Declarations: []DeclarationSpec{
			{
				NodeType:     "namespace_definition",
				Kind:         "package",
				NameNodeType: "namespace_name",
				Package:      true,
			},
			{
				NodeType:   "class_declaration",
				Kind:       "class",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:   "interface_declaration",
				Kind:       "interface",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:   "trait_declaration",
				Kind:       "trait",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:      "method_declaration",
				Kind:          "function",
				NameField:     "name",
				FunctionScope: true,
				MethodInClass: true,
			},
			{
				NodeType:      "function_definition",
				Kind:          "function",
				NameField:     "name",
				FunctionScope: true,
			},
		},
		Calls: []CallSpec{
			{NodeType: "function_call_expression", FunctionField: "function"},
			{NodeType: "object_creation_expression", CalleeNodeTypes: []string{"name", "qualified_name"}},
		},
		InspectHook: phpInspectHook,
	}
}

// phpInspectHook emits "import" entities for namespace_use_declaration
// nodes, handling both simple clauses and grouped imports
// ("use A\B\{C, D as E}"), stripping any "as" alias from the clause text.
func phpInspectHook(w *Walker, n *sitter.Node) {
	if n.Type(w.lang()) != "namespace_use_declaration" {
		return
	}
	if group := w.childOfType(n, "namespace_use_group"); group != nil {
		base := ""
		if nn := w.childOfType(n, "namespace_name"); nn != nil {
			base = strings.TrimSpace(string(w.content[nn.StartByte():nn.EndByte()])) + "\\"
		}
		for _, clause := range w.descendantsOfType(group, "namespace_use_clause") {
			w.emitPHPImport(n, clause, base)
		}
		return
	}
	if clause := w.childOfType(n, "namespace_use_clause"); clause != nil {
		w.emitPHPImport(n, clause, "")
	}
}

// emitPHPImport registers one imported module from a use clause.
func (w *Walker) emitPHPImport(n *sitter.Node, clause *sitter.Node, prefix string) {
	module := strings.TrimSpace(string(w.content[clause.StartByte():clause.EndByte()]))
	if i := strings.Index(module, " as "); i >= 0 {
		module = module[:i]
	}
	module = prefix + module
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
