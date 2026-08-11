package parser

import (
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractKotlinData parses a Kotlin file, pulling out imports, classes,
// interfaces, enums, objects, functions, methods, and within-file call
// links.
func extractKotlinData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, kotlinConfig())
}

// kotlinConfig maps the tree-sitter-kotlin grammar onto the shared
// walker.  Notes on the grammar:
//
//   - class_declaration is a single node type for classes, interfaces,
//     and enums; KindByChild switches the entity kind by looking at the
//     "interface"/"enum" keyword children.  Names live in a
//     type_identifier child (no field).
//   - object_declaration covers "object Foo" singletons.
//   - function_declaration names live in the first simple_identifier
//     child; MethodInClass makes class-scoped ones "Class.method".
//   - import_header (inside import_list) carries the path in an
//     identifier child.
//   - call_expression has no function field; the callee is the first
//     simple_identifier direct child (navigation_expression member calls
//     do not match, and "helper()" inside a class resolves to
//     "Class.helper" via the class-scoped fallback).
func kotlinConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.KotlinLanguage(),
		Declarations: []DeclarationSpec{
			{
				NodeType:     "package_header",
				Kind:         "package",
				NameNodeType: "identifier",
				Package:      true,
			},
			{
				NodeType:     "class_declaration",
				Kind:         "class",
				NameNodeType: "type_identifier",
				ClassScope:   true,
				KindByChild: map[string]string{
					"interface": "interface",
					"enum":      "enum",
				},
			},
			{
				NodeType:     "object_declaration",
				Kind:         "object",
				NameNodeType: "type_identifier",
				ClassScope:   true,
			},
			{
				NodeType:      "function_declaration",
				Kind:          "function",
				NameNodeType:  "simple_identifier",
				FunctionScope: true,
				MethodInClass: true,
			},
		},
		Imports: []ImportSpec{
			{NodeType: "import_header", SourceChildTypes: []string{"identifier"}},
		},
		RefNodeTypes: []string{"type_identifier"},
		Calls: []CallSpec{
			{NodeType: "call_expression", CalleeNodeTypes: []string{"simple_identifier"}},
		},
	}
}
