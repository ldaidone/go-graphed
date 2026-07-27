package cli

import (
	"strings"

	"github.com/ldaidone/go-graphed/pkg/graphed"
	"github.com/spf13/cobra"
)

var (
	output string
	format string
)

// buildCmd is registered under rootCmd in init() below.
// Keeping the command definition and its flag wiring together
// makes it easy to see everything a subcommand needs in one place.
var buildCmd = &cobra.Command{
	Use:   "build <source>",
	Short: "Build a knowledge graph",
	Args:  cobra.ExactArgs(1),

	RunE: func(cmd *cobra.Command, args []string) error {
		var opts graphed.BuildOptions
		opts = graphed.BuildOptions{
			Root:   cleanSourcePath(args[0]),
			Output: output,
			Format: format,
		}
		return graphed.Build(opts)
	},
}

// cleanSourcePath sanitizes input paths, converting Go-style "./..." wildcards
// into standard directories that standard file systems understand.
// Users accustomed to "go build ./..." often pass that syntax here,
// so we silently normalise it rather than rejecting it.
func cleanSourcePath(source string) string {
	// If the user specifies the recursive wildcard syntax, e.g., "./..." or "internal/..."
	if strings.HasSuffix(source, "/...") {
		// Strip off the "/..." suffix to target the parent directory directly
		source = strings.TrimSuffix(source, "/...")
	} else if source == "..." {
		// If they literally just typed "...", treat it as the current directory "."
		source = "."
	}

	// Fallback in case trimming leaves it blank (e.g. from "/...")
	if source == "" {
		source = "."
	}

	return source
}

// init registers the build subcommand and its flags on the root command.
// Using init() here is idiomatic for cobra CLI apps: each file owns its
// own subcommand registration without cluttering main().
func init() {

	buildCmd.Flags().StringVarP(
		&output,
		"output",
		"o",
		"graph.json",
		"Output file",
	)

	buildCmd.Flags().StringVar(
		&format,
		"format",
		"json",
		"Output format",
	)

	rootCmd.AddCommand(buildCmd)
}
