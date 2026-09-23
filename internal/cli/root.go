// Package cli wires the cobra command tree and exposes Execute
// so that cmd/kg/main.go never imports cobra directly.
package cli

import "github.com/spf13/cobra"

// Version is the CLI release version reported by `kg --version`.
// Overridden at link time via -ldflags "-X .../cli.Version=...".
var Version = "1.0.0"

// RootCmd is the top-level command that every subcommand attaches to.
// Defining it here (rather than in an init func) keeps the
// registration order explicit and avoids hidden side effects.
var RootCmd = &cobra.Command{
	Use:     "kg",
	Short:   "Generate knowledge graphs from source code",
	Version: Version,
	Long: `go-graphed analyzes source code and generates
language-agnostic knowledge graphs.`,
}

// Command groups registered on RootCmd so `--help` renders commands in
// stable operational tiers.
var (
	// CoreGroup collects the core graph commands (build, visualize, ...).
	CoreGroup = &cobra.Group{ID: "core", Title: "Core Commands:"}
	// InfraGroup collects the infrastructure protocol commands (mcp, ...).
	InfraGroup = &cobra.Group{ID: "infra", Title: "Infrastructure Protocols:"}
	// SetupGroup collects the setup commands (init, install, ...).
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
