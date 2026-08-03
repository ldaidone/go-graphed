// Package cli wires the cobra command tree and exposes Execute
// so that cmd/kg/main.go never imports cobra directly.
package cli

import "github.com/spf13/cobra"

// RootCmd is the top-level command that every subcommand attaches to.
// Defining it here (rather than in an init func) keeps the
// registration order explicit and avoids hidden side effects.
var RootCmd = &cobra.Command{
	Use:   "kg",
	Short: "Generate knowledge graphs from source code",
	Long: `go-graphed analyzes source code and generates
language-agnostic knowledge graphs.`,
}

// 1. Declare the operational tiers as package variables
var (
	CoreGroup  = &cobra.Group{ID: "core", Title: "Core Commands:"}
	InfraGroup = &cobra.Group{ID: "infra", Title: "Infrastructure Protocols:"}
	SetupGroup = &cobra.Group{ID: "setup", Title: "Setup Commands:"}
)

func init() {
	// 2. Register groups directly onto the root entry point
	RootCmd.AddGroup(CoreGroup, InfraGroup, SetupGroup)
}

// Execute runs the root command. Separating this from main() lets
// the CLI layer be exercised independently in future tests.
func Execute() error {
	return RootCmd.Execute()
}
