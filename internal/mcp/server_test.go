package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/utils/vector_store"
	mcp_golang "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport/stdio"
)

// fakeCloseEmbedder is a TextEmbedder that records Close calls.
type fakeCloseEmbedder struct{ closed int }

func (f *fakeCloseEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	return nil, nil
}

func (f *fakeCloseEmbedder) Close(ctx context.Context) error {
	f.closed++
	return nil
}

func TestNewServer_NoModelResolved(t *testing.T) {
	// Isolate every model source: env, cwd, and the user config dir.
	t.Setenv(config.EnvModelPath, "")
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	_, err := NewServer(&ir.Graph{}, Options{DBRoot: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error when no model is resolvable")
	}
	if !strings.Contains(err.Error(), "no embedding model resolved") {
		t.Errorf("error = %q, want it to mention the missing model", err)
	}
}

func TestNewServer_BadDBRoot(t *testing.T) {
	// HOME unset makes the default DBRoot unresolvable -> NewServer must
	// fail before it ever touches an embedding model.
	t.Setenv("HOME", "")
	t.Setenv(config.EnvModelPath, "")

	_, err := NewServer(&ir.Graph{}, Options{})
	if err == nil {
		t.Fatal("expected an error when the database path cannot be resolved")
	}
	if !strings.Contains(err.Error(), "failed to determine database path") {
		t.Errorf("error = %q, want it to mention the database path", err)
	}
}

func TestRegisterTools(t *testing.T) {
	s := &Server{
		metoroServer: mcp_golang.NewServer(stdio.NewStdioServerTransport()),
	}
	if err := s.registerTools(); err != nil {
		t.Fatalf("registerTools returned error: %v", err)
	}
}

func TestClose_ReleasesResourcesAndIsIdempotent(t *testing.T) {
	store, err := vector_store.NewBadgerStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBadgerStore returned error: %v", err)
	}
	embedder := &fakeCloseEmbedder{}

	s := &Server{store: store, embedder: embedder}

	s.Close()
	if s.store != nil {
		t.Error("store was not nilled out after Close")
	}
	if s.embedder != nil {
		t.Error("embedder was not nilled out after Close")
	}
	if embedder.closed != 1 {
		t.Errorf("embedder Close calls = %d, want 1", embedder.closed)
	}

	// A second Close is a no-op.
	s.Close()
	if embedder.closed != 1 {
		t.Errorf("embedder Close calls after second Close = %d, want still 1", embedder.closed)
	}
}

func TestClose_NilFields(t *testing.T) {
	s := &Server{} // zero value: no store, no embedder
	s.Close()      // must not panic
}

func TestStart_CancelledContextReturnsNil(t *testing.T) {
	s := &Server{
		metoroServer: mcp_golang.NewServer(stdio.NewStdioServerTransport()),
		graph:        &ir.Graph{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled: Start must exit promptly without an error.

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start with cancelled context returned error: %v", err)
	}
}

func TestNewServer_BadDBRootFailsFast(t *testing.T) {
	// A DBRoot whose parent path cannot be created (root exists as a file)
	// must surface as an initialization error, not a panic.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(config.EnvModelPath, "/some/model.gtemodel")
	_, err := NewServer(&ir.Graph{}, Options{DBRoot: blocker})
	if err == nil {
		t.Fatal("expected an error when DBRoot cannot be created")
	}
}
