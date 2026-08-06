package parser

import (
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractJavaData parses a Java file, pulling out imports, classes,
// interfaces, enums, methods, and within-file call links.
func extractJavaData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, javaConfig())
}

// javaConfig maps the tree-sitter-java grammar onto the shared walker.
// Notes on the grammar:
//
//   - import_declaration carries the import path in a scoped_identifier
//     direct child.
//   - classes, interfaces, and enums all declare a name field and act as
//     scopes, so nested classes and methods get "Outer.Inner" and
//     "Class.method" names.
//   - method_declaration covers both concrete methods (with a body) and
//     interface/abstract signatures (without one); MethodInClass makes
//     every method a "Class.method" entity.
//   - Calls come in two node types: method_invocation for "helper()" and
//     object_creation_expression for "new UserService()" (which resolves
//     to the class entity).
func javaConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.JavaLanguage(),
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
				NodeType:   "enum_declaration",
				Kind:       "enum",
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
			{NodeType: "import_declaration", SourceChildTypes: []string{"scoped_identifier"}},
		},
		Calls: []CallSpec{
			{NodeType: "method_invocation", FunctionField: "name", ObjectField: "object"},
			{NodeType: "object_creation_expression", FunctionField: "type"},
		},
	}
}
