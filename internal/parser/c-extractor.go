package parser

import (
	"fmt"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractCData parses a C file, pulling out #include directives,
// functions, structs, unions, enums, typedefs, and within-file call
// links.
func extractCData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, cConfig())
}

// cConfig maps the tree-sitter-c grammar onto the shared walker.  Notes
// on the grammar:
//
//   - function names live inside the function_definition's
//     declarator → declarator path, so they use NameFieldPath.
//   - struct/union/enum names live in the "name" field; typedef names
//     live in the "declarator" field.
//   - #include cannot be an ImportSpec row (its path may be a
//     system_lib_string, string_literal, or identifier child), so
//     cIncludeHook emits it.
//   - call_expression carries the callee in its "function" field; member
//     calls (obj.method(), ptr->method()) wrap the receiver in a
//     field_expression whose qualified text never matches a bare symbol.
func cConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.CLanguage(),
		Declarations: []DeclarationSpec{
			{
				NodeType:      "function_definition",
				Kind:          "function",
				NameFieldPath: []string{"declarator", "declarator"},
				FunctionScope: true,
			},
			{
				NodeType:  "struct_specifier",
				Kind:      "struct",
				NameField: "name",
			},
			{
				NodeType:  "union_specifier",
				Kind:      "union",
				NameField: "name",
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
		RefNodeTypes: []string{"type_identifier"},
		InspectHook:  cIncludeHook,
	}
}

// cIncludeHook emits an "import" entity for a preprocessor #include,
// normalizing both <angle> and "quoted" path forms.  The include_kind
// metadata distinguishes local "quoted" headers (resolvable against the
// indexed tree) from <angle> system headers (external, left unlinked by
// the analyzer's import-resolution pass).
func cIncludeHook(w *Walker, n *sitter.Node) {
	if n.Type(w.lang()) != "preproc_include" {
		return
	}
	module := ""
	kind := "system"
	for _, t := range []string{"system_lib_string", "string_literal", "identifier"} {
		if c := w.childOfType(n, t); c != nil {
			module = strings.TrimSpace(string(w.content[c.StartByte():c.EndByte()]))
			if t == "string_literal" {
				kind = "local"
			}
			break
		}
	}
	module = strings.Trim(module, "<>\"")
	if module == "" {
		return
	}
	w.entities = append(w.entities, ir.Entity{
		ID:       fmt.Sprintf("%s#import:%s", w.path, module),
		Type:     "import",
		Name:     module,
		Metadata: lineMetadata(n),
	})
	w.entities[len(w.entities)-1].Metadata["include_kind"] = kind
}
