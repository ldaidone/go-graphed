package cli

import (
	"strings"

	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/ldaidone/go-graphed/pkg/graphed"
	"github.com/spf13/cobra"
)

var (
	output        string
	format        string
	modelPath     string
	dimensions    int
	jobs          int
	dbRoot        string
	exclude       []string
	ignoreFiles   []string
	noGitIgnore   bool
	noDefaultSkip bool
	skipEmbed     []string
)

// buildCmd is registered under RootCmd in init() below.
// Keeping the command definition and its flag wiring together
// makes it easy to see everything a subcommand needs in one place.
var buildCmd = &cobra.Command{
	Use:     "build <source>",
	Short:   "Build a knowledge graph",
	Args:    cobra.ExactArgs(1),
	GroupID: "core",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(config.Overrides{
			ModelPath:  modelPath,
			Dimensions: dimensions,
			DBRoot:     dbRoot,
		})
		if err != nil {
			return err
		}

		var opts graphed.BuildOptions
		opts = graphed.BuildOptions{
			Root:           cleanSourcePath(args[0]),
			Output:         output,
			Format:         format,
			ModelPath:      cfg.ModelPath,
			Dimensions:     cfg.Dimensions,
			DBRoot:         cfg.DBRoot,
			Jobs:           jobs,
			Exclude:        exclude,
			IgnoreFiles:    ignoreFiles,
			NoGitIgnore:    noGitIgnore,
			NoDefaultSkip:  noDefaultSkip,
			SkipEmbedTypes: skipEmbed,
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

	buildCmd.Flags().StringVar(
		&modelPath,
		"model-path",
		"",
		"Path to a gte embedding model (defaults to GRAPHEAD_MODEL_PATH or ./get-small.gtemodel)",
	)

	buildCmd.Flags().IntVar(
		&dimensions,
		"dimensions",
		0,
		"Embedding vector width of the model (0 = auto-detect from the model)",
	)

	buildCmd.Flags().StringVar(
		&dbRoot,
		"db-root",
		"",
		"Base config directory for the vector store (defaults to ~/.config/graphed)",
	)

	buildCmd.Flags().IntVar(
		&jobs,
		"jobs",
		0,
		"Parser worker count (0 = runtime.NumCPU())",
	)

	buildCmd.Flags().StringSliceVar(
		&skipEmbed,
		"embed-skip-types",
		graphed.DefaultSkipEmbedTypes,
		"Entity types to skip when generating embeddings (import/package are always skipped; pass \"\" to embed every entity type)",
	)

	buildCmd.Flags().StringArrayVar(
		&exclude,
		"exclude",
		nil,
		"Extra gitignore-style pattern to exclude (repeatable, e.g. --exclude vendor/ --exclude '*.gen.go')",
	)

	buildCmd.Flags().StringArrayVar(
		&ignoreFiles,
		"ignore-file",
		nil,
		"Additional gitignore-format file to apply (repeatable, relative to the source root)",
	)

	buildCmd.Flags().BoolVar(
		&noGitIgnore,
		"no-git-ignore",
		false,
		"Disable discovery and application of .gitignore files",
	)

	buildCmd.Flags().BoolVar(
		&noDefaultSkip,
		"no-default-skip",
		false,
		"Disable the built-in noise filter (VCS internals, binary assets, lockfiles)",
	)

	RootCmd.AddCommand(buildCmd)
}
