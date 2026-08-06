package parser

import (
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractSwiftData parses a Swift file, pulling out imports, classes,
// structs, enums, protocols, methods, and within-file call links.
func extractSwiftData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, swiftConfig())
}

// swiftConfig maps the tree-sitter-swift grammar onto the shared
// walker.  Notes on the grammar:
//
//   - class_declaration is a single node type for classes, structs, and
//     enums; KindByChild switches the entity kind by looking at the
//     leading keyword child.  All of them use the "name" field and act
//     as scopes, so nested types and methods get "Outer.Inner" and
//     "Class.method" names.
//   - protocol_declaration and function-like declarations carry their
//     name in the "name" field.
//   - import_declaration carries the module path in an identifier or
//     scoped_identifier direct child.
//   - call_expression has no function field; a bare call is the direct
//     simple_identifier child (member calls are wrapped in a
//     member_access_expression and never match, so "helper()" inside a
//     class resolves to "Class.helper" via the class-scoped fallback).
func swiftConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.SwiftLanguage(),
		Declarations: []DeclarationSpec{
			{
				NodeType:   "class_declaration",
				Kind:       "class",
				NameField:  "name",
				ClassScope: true,
				KindByChild: map[string]string{
					"class":  "class",
					"struct": "struct",
					"enum":   "enum",
				},
			},
			{
				NodeType:   "protocol_declaration",
				Kind:       "protocol",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:   "actor_declaration",
				Kind:       "actor",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:      "function_declaration",
				Kind:          "function",
				NameField:     "name",
				FunctionScope: true,
				MethodInClass: true,
			},
			{
				NodeType:      "protocol_function_declaration",
				Kind:          "function",
				NameField:     "name",
				MethodInClass: true,
			},
		},
		Imports: []ImportSpec{
			{NodeType: "import_declaration", SourceChildTypes: []string{"identifier", "scoped_identifier"}},
		},
		Calls: []CallSpec{
			{NodeType: "call_expression", CalleeNodeTypes: []string{"simple_identifier"}},
		},
	}
}
