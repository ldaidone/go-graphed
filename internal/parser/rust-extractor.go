package parser

import (
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractRustData parses a Rust file, pulling out use declarations,
// modules, structs, enums, traits, functions, impl-block methods, and
// within-file call links.
func extractRustData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, rustConfig())
}

// rustConfig maps the tree-sitter-rust grammar onto the shared walker.
// Notes on the grammar:
//
//   - function_item is used for both free functions and impl-block
//     methods; MethodInClass turns impl-scoped ones into "method"
//     entities named "Type.name" (the impl block's "type" field names
//     the scope).
//   - impl_item emits no entity itself -- it is a ScopeOnly container
//     so its methods get "Type.method" names.  trait_item is a real
//     entity whose function_signature_item children become
//     "Trait.method" methods.
//   - use_declaration names a module through a scoped_identifier child
//     (its "path" for braced import lists).
//   - call_expression callees that are bare identifiers resolve to
//     within-file symbols; "Point::new" and "obj.method" callees carry
//     qualified text and never match.
func rustConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.RustLanguage(),
		Declarations: []DeclarationSpec{
			{
				NodeType:      "function_item",
				Kind:          "function",
				NameField:     "name",
				FunctionScope: true,
				MethodInClass: true,
			},
			{
				NodeType:      "function_signature_item",
				Kind:          "function",
				NameField:     "name",
				MethodInClass: true,
			},
			{
				NodeType:  "struct_item",
				Kind:      "struct",
				NameField: "name",
			},
			{
				NodeType:  "enum_item",
				Kind:      "enum",
				NameField: "name",
			},
			{
				NodeType:   "trait_item",
				Kind:       "trait",
				NameField:  "name",
				ClassScope: true,
			},
			{
				NodeType:   "impl_item",
				Kind:       "impl",
				NameField:  "type",
				ClassScope: true,
				ScopeOnly:  true,
			},
			{
				NodeType:  "mod_item",
				Kind:      "module",
				NameField: "name",
			},
		},
		Imports: []ImportSpec{
			{NodeType: "use_declaration", SourceNodeType: "scoped_identifier"},
		},
		Calls: []CallSpec{
			{
				NodeType:      "call_expression",
				FunctionField: "function",
			},
		},
	}
}
