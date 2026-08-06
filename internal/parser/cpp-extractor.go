package parser

import (
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractCppData parses a C++ file, pulling out #include directives,
// namespaces, classes, structs, enums, typedefs, functions, methods,
// and within-file call links.
func extractCppData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, cppConfig())
}

// cppConfig maps the tree-sitter-cpp grammar onto the shared walker.
// Notes on the grammar:
//
//   - function_definition carries its name in the declarator →
//     declarator path (an identifier for free functions, a
//     field_identifier for methods); NameFieldPath handles both.
//     MethodInClass renames class-scoped ones to "Class.method".
//   - namespace_definition, class_specifier, struct_specifier, and
//     enum_specifier use the "name" field and (except enums) act as
//     scopes, so nested types and methods get "ns.Outer.Inner" and
//     "ns.Class.method" names.  typedef names live in the "declarator"
//     field.
//   - #include is handled by the shared cIncludeHook.
//   - call_expression callees come from the "function" field; member
//     calls (c.area(), ptr->area()) and qualified calls (ns::foo())
//     carry qualified text that never matches a bare symbol.
func cppConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.CppLanguage(),
		Declarations: []DeclarationSpec{
			{
				NodeType:      "function_definition",
				Kind:          "function",
				NameFieldPath: []string{"declarator", "declarator"},
				FunctionScope: true,
				MethodInClass: true,
			},
			{
				NodeType:   "namespace_definition",
				Kind:       "namespace",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:   "class_specifier",
				Kind:       "class",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:   "struct_specifier",
				Kind:       "struct",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:  "enum_specifier",
				Kind:      "enum",
				NameField: "name",
			},
			{
				NodeType:  "type_definition",
				Kind:      "typedef",
				NameField: "declarator",
			},
		},
		Calls: []CallSpec{
			{NodeType: "call_expression", FunctionField: "function"},
		},
		InspectHook: cIncludeHook,
	}
}
