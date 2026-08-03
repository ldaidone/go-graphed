package cli

import (
	"context"
	"fmt"
	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/ldaidone/go-graphed/internal/exporter"
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/mcp"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var (
	graphPath    string
	mcpModelPath string
	mcpDBRoot    string
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
			ModelPath: cfg.ModelPath,
			DBRoot:    cfg.DBRoot,
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

	RootCmd.AddCommand(mcpCmd)
}
