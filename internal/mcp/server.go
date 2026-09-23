package mcp

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ldaidone/go-graphed/internal/analyzer"
	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/ldaidone/go-graphed/internal/git"
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/goembedx/pkg/embedx"
	"github.com/ldaidone/goembedx/pkg/store"
	"github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport/stdio"
)

// TextEmbedder is the subset of analyzer.NativeEmbedder the MCP layer
// needs. Declaring an interface here (instead of the concrete type)
// lets tests inject lightweight fakes without loading a real model.
type TextEmbedder interface {
	EmbedText(ctx context.Context, text string) ([]float32, error)
	Close(ctx context.Context) error
}

// Options carries the runtime configuration the server needs to build its
// semantic search engine. Zero values fall back to the default resolution
// chain implemented in internal/config.
type Options struct {
	// ModelPath is a gte embedding model path. When empty it is resolved
	// via config.ResolveModelPath; an unresolvable model is an error since
	// the semantic search tools require it.
	ModelPath string

	// DBRoot is the base config directory for the vector store. When empty
	// the default under the user's home directory is used.
	DBRoot string

	// AutoRebuild enables the freshness bouncer: each request cheaply
	// checks the git fingerprint of Root, and a dirty tree kicks one
	// debounced background rebuild while the current snapshot serves.
	// Default off; explicit `kg build` stays the predictable default.
	AutoRebuild bool

	// Root is the source tree to fingerprint and rebuild. Required when
	// AutoRebuild is true.
	Root string

	// GraphFile is the snapshot path reloaded after a rebuild.
	GraphFile string

	// GitLimit caps recent commits mined for co-change links on rebuild.
	GitLimit int

	// Rebuild runs the full pipeline; the CLI wires it to graphed.Build.
	// Nil disables auto-rebuild even when AutoRebuild is true.
	Rebuild func() error

	// CheckTTL and Debounce tune the bouncer; zero selects defaults.
	// Exposed for tests.
	CheckTTL time.Duration
	Debounce time.Duration
}

// Server is the MCP protocol controller exposing a loaded graph as JSON-RPC
// tools over stdio. It owns the vector store and the embedding model for the
// server's lifetime; call Close (or let Start's deferred cleanup run) to
// release them.
type Server struct {
	metoroServer *mcp_golang.Server
	mu           sync.RWMutex // guards graph across handlers + background swaps
	graph        *ir.Graph
	searcher     embedx.Searcher // Semantic retrieval over the vector store
	embedder     TextEmbedder    // Native pure-Go embedder wrapper
	store        *store.SQLite   // Kept alive for the server's lifetime

	autoRebuild     bool
	refresh         RefreshConfig
	refreshTTL      time.Duration
	refreshDebounce time.Duration
	fresh           freshness
}

// NewServer builds an instance of the MCP protocol controller bound to the
// loaded graph IR. The returned Server owns the vector store and the embedding
// model; call Close (or let Start's deferred cleanup run) to release them.
func NewServer(graph *ir.Graph, opts Options) (*Server, error) {
	// Initialize the protocol over stdio transport stream
	transport := stdio.NewStdioServerTransport()

	dbPath, err := config.BadgerDBPath(opts.DBRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to determine database path: %w", err)
	}

	st, err := store.NewSQLite(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize vector store: %w", err)
	}

	modelPath := config.ResolveModelPath(opts.ModelPath)
	if modelPath == "" {
		// Close the store we just opened so we don't leak file handles on failure.
		_ = st.Close()
		return nil, fmt.Errorf("no embedding model resolved: set --model-path or GRAPHEAD_MODEL_PATH")
	}

	ctx := context.Background()
	embedder, err := analyzer.NewNativeEmbedder(ctx, modelPath)
	if err != nil {
		// Close the store we just opened so we don't leak file handles on failure.
		_ = st.Close()
		return nil, fmt.Errorf("failed to load native semantic embedding engine: %w", err)
	}

	return &Server{
		metoroServer: mcp_golang.NewServer(transport),
		graph:        graph,
		searcher:     st,
		embedder:     embedder,
		store:        st,
		autoRebuild:  opts.AutoRebuild,
		refresh: RefreshConfig{
			Root:      opts.Root,
			GraphFile: opts.GraphFile,
			Rebuild:   opts.Rebuild,
		},
		refreshTTL:      opts.CheckTTL,
		refreshDebounce: opts.Debounce,
		fresh:           freshness{lastFP: initialFingerprint(opts)},
	}, nil
}

// initialFingerprint records the tree state at startup so a fresh graph
// does not trigger a redundant rebuild on the first request. Failures
// (non-repo) leave it empty, meaning the first dirty check just records.
func initialFingerprint(opts Options) string {
	if !opts.AutoRebuild || opts.Root == "" {
		return ""
	}
	fp, err := git.Fingerprint(opts.Root)
	if err != nil {
		return ""
	}
	return fp
}

// Close releases the vector store and the native embedding model.
// Safe to call multiple times.
func (s *Server) Close() {
	if s.store != nil {
		_ = s.store.Close()
		s.store = nil
	}
	if s.embedder != nil {
		_ = s.embedder.Close(context.Background())
		s.embedder = nil
	}
}

// Start registers dependency handlers and coordinates blocking execution against contextual shifts
func (s *Server) Start(ctx context.Context) error {
	// Ensure the resources created in NewServer are released no matter how Start exits.
	defer s.Close()

	fmt.Fprintln(os.Stderr, "Registering structural graph query tools...")

	// Wire tool endpoints to our handlers logic
	if err := s.registerTools(); err != nil {
		return fmt.Errorf("failed to register tools: %w", err)
	}

	serverErrChan := make(chan error, 1)

	// Execute Metoro server operations in a decoupled goroutine execution path
	// with panic recovery so a handler bug surfaces as an error, not a dead server.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				serverErrChan <- fmt.Errorf("mcp server panicked: %v", r)
			}
		}()
		fmt.Fprintln(os.Stderr, "MCP Server listening on stdin/stdout...")
		if err := s.metoroServer.Serve(); err != nil {
			serverErrChan <- err
		}
	}()

	// Watch for context cancellation or unexpected server failure loops
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "Context cancelled. Terminating server execution loop gracefully...")
		// Metoro framework handles connection tear down natively when underlying transport terminates.
		return nil
	case err := <-serverErrChan:
		return fmt.Errorf("mcp runtime execution error: %w", err)
	}
}
