package parser

import (
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractCSharpData parses a C# file, pulling out using directives,
// classes, interfaces, structs, enums, records, methods, and within-file
// call links.
func extractCSharpData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, csharpConfig())
}

// csharpConfig maps the tree-sitter-csharp grammar onto the shared
// walker.  Notes on the grammar:
//
//   - using_directive carries the namespace in a qualified_name or bare
//     identifier direct child.
//   - classes, interfaces, structs, enums, and records declare a name
//     field and act as scopes, so nested types and methods get
//     "Outer.Inner" and "Class.method" names.
//   - method_declaration covers both implemented and interface/abstract
//     signatures.
//   - Calls: invocation_expression for "Helper()" (resolves via the
//     class-scoped fallback) and object_creation_expression for
//     "new UserService()" (resolves to the class entity).  Member calls
//     carry qualified "obj.Method" text and never match.
func csharpConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.CSharpLanguage(),
		Declarations: []DeclarationSpec{
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
				NodeType:   "struct_declaration",
				Kind:       "struct",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:   "enum_declaration",
				Kind:       "enum",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:   "record_declaration",
				Kind:       "record",
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
		},
		Imports: []ImportSpec{
			{NodeType: "using_directive", SourceChildTypes: []string{"qualified_name", "identifier"}},
		},
		Calls: []CallSpec{
			{NodeType: "invocation_expression", FunctionField: "function"},
			{NodeType: "object_creation_expression", FunctionField: "type"},
		},
	}
}
