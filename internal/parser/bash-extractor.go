package parser

import (
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractBashData parses a shell script, pulling out function
// definitions and within-file call links between locally declared
// functions.
func extractBashData(path string) ([]ir.Entity, []ir.Link, error) {
	return extractWithConfig(path, bashConfig())
}

// bashConfig maps the tree-sitter-bash grammar onto the shared walker.
// Notes on the grammar:
//
//   - function_definition carries the name in a "name" field of node
//     type "word".
//   - There is no call-expression node; a command invocation is a
//     "command" node whose "name" field (command_name) holds the invoked
//     program or function.  emitCall resolves the command text against
//     the functions declared in the file, so only local function calls
//     produce "calls" links -- external programs (make, git, ...) never
//     match.
//
// Variable assignments are deliberately not extracted: they add noise,
// and unqualified names collide across function-local scopes.
func bashConfig() *LanguageConfig {
	return &LanguageConfig{
		Language: grammars.BashLanguage(),
		Declarations: []DeclarationSpec{
			{
				NodeType:      "function_definition",
				Kind:          "function",
				NameField:     "name",
				FunctionScope: true,
			},
		},
		Calls: []CallSpec{
			{
				NodeType:      "command",
				FunctionField: "name",
			},
		},
		SkipNodeTypes: map[string]bool{"comment": true},
	}
}
