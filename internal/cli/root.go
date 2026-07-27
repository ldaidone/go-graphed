// Package cli wires the cobra command tree and exposes Execute
// so that cmd/kg/main.go never imports cobra directly.
package cli

import "github.com/spf13/cobra"

// rootCmd is the top-level command that every subcommand attaches to.
// Defining it here (rather than in an init func) keeps the
// registration order explicit and avoids hidden side effects.
var rootCmd = &cobra.Command{
	Use:   "kg",
	Short: "Generate knowledge graphs from source code",
	Long: `go-graphed analyzes source code and generates
language-agnostic knowledge graphs.`,
}

// Execute runs the root command. Separating this from main() lets
// the CLI layer be exercised independently in future tests.
func Execute() error {
	return rootCmd.Execute()
}
