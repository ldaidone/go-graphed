package parser

import (
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractPythonData parses a Python file, pulling out imports,
// functions, classes, methods, and within-file call links.  It is the
// pilot consumer of the shared config-driven walker.
func extractPythonData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, pythonConfig())
}

// pythonConfig maps the tree-sitter-python grammar onto the shared
// walker.  Notes on the grammar:
//
//   - class_definition carries the class name and body block.
//   - function_definition is used for both plain functions and class
//     methods; MethodInClass turns class-scoped ones into "method"
//     entities named "Class.name".
//   - decorated_definition wraps a class/function with decorators but
//     keeps the inner declaration as a "definition" child, so generic
//     recursion extracts it without special handling.
//   - import_statement names a module through a dotted_name child
//     ("import os.path"), while import_from_statement carries it in the
//     module_name field ("from typing import X").
//   - call nodes hold the callee in the "function" field; only bare
//     identifier callees resolve to within-file symbols.
//
// The comment node type is skipped because comments cannot contain
// declarations or calls; the full corpus of literal types is left
// alone so calls inside f-string interpolations still resolve.
func pythonConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.PythonLanguage(),
		Declarations: []DeclarationSpec{
			{
				NodeType:   "class_definition",
				Kind:       "class",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:      "function_definition",
				Kind:          "function",
				NameField:     "name",
				FunctionScope: true,
				MethodInClass: true,
			},
		},
		Imports: []ImportSpec{
			{NodeType: "import_statement", SourceNodeType: "dotted_name"},
			{NodeType: "import_from_statement", SourceField: "module_name"},
		},
		Calls: []CallSpec{
			{
				NodeType:      "call",
				FunctionField: "function",
			},
		},
		SkipNodeTypes: map[string]bool{"comment": true},
	}
}
