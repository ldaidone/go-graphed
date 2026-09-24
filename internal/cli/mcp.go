package cli

import (
	"context"
	"fmt"
	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/ldaidone/go-graphed/internal/exporter"
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/mcp"
	"github.com/ldaidone/go-graphed/pkg/graphed"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var (
	graphPath      string
	mcpModelPath   string
	mcpDBRoot      string
	mcpRoot        string
	mcpAutoBuild   bool
	mcpGitLimit    int
	mcpChangedOnly bool
)

var mcpCmd = &cobra.Command{
	Use:     "mcp",
	Short:   "Start the code graph MCP server over standard I/O streams",
	GroupID: "infra",
	Run: func(cmd *cobra.Command, args []string) {
		var err error
		var cfg config.Settings
		var graph *ir.Graph
		var srv *mcp.Server

		fmt.Fprintln(os.Stderr, "Loading AST Graph data store...")

		graph, err = exporter.LoadGraph(graphPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing graph target asset: %v\n", err)
			os.Exit(1)
		}

		// Instantiate the clean, decoupled service
		cfg, err = config.Load(config.Overrides{
			ModelPath: mcpModelPath,
			DBRoot:    mcpDBRoot,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving configuration: %v\n", err)
			os.Exit(1)
		}

		srv, err = mcp.NewServer(graph, mcp.Options{
			ModelPath:   cfg.ModelPath,
			DBRoot:      cfg.DBRoot,
			AutoRebuild: mcpAutoBuild,
			Root:        mcpRoot,
			GraphFile:   graphPath,
			Rebuild:     mcpRebuildFunc(cfg, graphPath),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing MCP server: %v\n", err)
			os.Exit(1)
		}

		// Setup OS Signal capturing channel metrics
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		// Create a cancelable context mapped to OS signals
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Listen for signals asynchronously to cancel the context
		go func() {
			sig := <-sigChan
			fmt.Fprintf(os.Stderr, "\nReceived OS signal: %s. Initiating graceful wind-down...\n", sig)
			cancel()
		}()

		// Start blocking execution loops passing down the cancellation context
		if err := srv.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Runtime server error terminated execution: %v\n", err)
			os.Exit(1)
		}

		// Add a short grace window if you need to flush log buffers to stderr
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintln(os.Stderr, "MCP engine shutdown complete. Exiting cleanly.")
		os.Exit(0)
	},
}

func init() {

	mcpCmd.Flags().StringVarP(
		&graphPath,
		"file",
		"f",
		"graph.json",
		"Source graph data JSON profile path",
	)

	mcpCmd.Flags().StringVar(
		&mcpModelPath,
		"model-path",
		"",
		"Path to a gte embedding model (defaults to GRAPHEAD_MODEL_PATH or ./get-small.gtemodel)",
	)

	mcpCmd.Flags().StringVar(
		&mcpDBRoot,
		"db-root",
		"",
		"Base config directory for the vector store (defaults to ~/.config/graphed)",
	)

	mcpCmd.Flags().BoolVar(
		&mcpAutoBuild,
		"auto-rebuild",
		false,
		"Watch the git fingerprint of --root and rebuild in the background when the tree changes (serves the current snapshot meanwhile)",
	)

	mcpCmd.Flags().StringVar(
		&mcpRoot,
		"root",
		".",
		"Source tree to fingerprint and rebuild when --auto-rebuild is set",
	)

	mcpCmd.Flags().IntVar(
		&mcpGitLimit,
		"git-limit",
		50,
		"Recent commits to mine for co-change links on auto-rebuild (0 = default 50)",
	)

	mcpCmd.Flags().BoolVar(
		&mcpChangedOnly,
		"changed-only",
		false,
		"Patch only working-tree-changed files on auto-rebuild (faster on large repos; safe since rebuilds merge into the snapshot)",
	)

	RootCmd.AddCommand(mcpCmd)
}

// mcpRebuildFunc returns the background rebuild closure for --auto-rebuild,
// or nil when disabled. Rebuilds are git-aware (status/HEAD metadata plus
// co_changed links) since the fingerprint that triggers them is git-based.
func mcpRebuildFunc(cfg config.Settings, output string) func() error {
	if !mcpAutoBuild {
		return nil
	}
	return func() error {
		return graphed.Build(graphed.BuildOptions{
			Root:        mcpRoot,
			Output:      output,
			Format:      "json",
			ModelPath:   cfg.ModelPath,
			Dimensions:  cfg.Dimensions,
			DBRoot:      cfg.DBRoot,
			GitAware:    true,
			ChangedOnly: mcpChangedOnly,
			GitLimit:    mcpGitLimit,
		})
	}
}
