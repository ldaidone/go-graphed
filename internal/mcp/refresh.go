package mcp

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ldaidone/go-graphed/internal/exporter"
	"github.com/ldaidone/go-graphed/internal/git"
	"github.com/ldaidone/go-graphed/internal/ir"
	mcp_golang "github.com/metoro-io/mcp-golang"
)

// Defaults for the auto-rebuild bouncer. The fingerprint check is cheap
// (one go-git status walk) but not free, so it is TTL-cached; the debounce
// coalesces editor save bursts into a single rebuild.
const (
	defaultCheckTTL = 2 * time.Second
	defaultDebounce = 2 * time.Second
)

// RefreshConfig carries everything the auto-rebuild bouncer needs.
// Rebuild runs the full build pipeline; Reload reads the fresh snapshot.
// Both are injected so tests can fake them and the future `query` command
// can reuse the fingerprint half without the rebuild half.
type RefreshConfig struct {
	// Root is the source tree to fingerprint and rebuild. Empty disables.
	Root string
	// GraphFile is the snapshot path reloaded after a rebuild.
	GraphFile string
	// Fingerprint defaults to git.Fingerprint; tests inject a stub.
	Fingerprint func(root string) (string, error)
	// Rebuild runs the full pipeline (wired by the CLI to graphed.Build).
	// Nil disables auto-rebuild.
	Rebuild func() error
	// Reload reads the fresh snapshot; defaults to exporter.LoadGraph.
	Reload func() (*ir.Graph, error)
}

// freshness state lives on Server (see server.go).
type freshness struct {
	mu        sync.Mutex
	lastFP    string
	lastCheck time.Time
	inFlight  atomic.Bool
}

func (s *Server) fingerprint() (string, error) {
	if s.refresh.Fingerprint != nil {
		return s.refresh.Fingerprint(s.refresh.Root)
	}
	return git.Fingerprint(s.refresh.Root)
}

func (s *Server) checkTTL() time.Duration {
	if s.refreshTTL > 0 {
		return s.refreshTTL
	}
	return defaultCheckTTL
}

func (s *Server) debounce() time.Duration {
	if s.refreshDebounce > 0 {
		return s.refreshDebounce
	}
	return defaultDebounce
}

// snapshot returns the current graph under read lock so background swaps
// never race concurrent handlers.
func (s *Server) snapshot() *ir.Graph {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.graph
}

// swapGraph replaces the served graph after a successful rebuild + reload.
func (s *Server) swapGraph(g *ir.Graph) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graph = g
}

// guarded wraps a tool handler with the freshness bouncer plus panic
// recovery: every request cheaply checks the git fingerprint, and a dirty
// tree kicks one debounced background rebuild (singleflight) while the
// current snapshot keeps serving. It never blocks the request on a build.
// A free function (not a method) because Go does not allow generic methods.
func guarded[A any](s *Server, name string, fn func(A) (*mcp_golang.ToolResponse, error)) func(A) (*mcp_golang.ToolResponse, error) {
	return func(args A) (*mcp_golang.ToolResponse, error) {
		s.maybeAutoRebuild()
		s.mu.RLock()
		defer s.mu.RUnlock()
		return withRecovery(name, fn)(args)
	}
}

// maybeAutoRebuild triggers at most one background rebuild when the tree
// fingerprint moved since the last check. It is a no-op unless auto-rebuild
// is enabled with a rebuild function and a root. Failures (non-repo,
// rebuild error, reload error) degrade to stderr warnings; the current
// snapshot keeps serving either way.
func (s *Server) maybeAutoRebuild() {
	if !s.autoRebuild || s.refresh.Rebuild == nil || s.refresh.Root == "" {
		return
	}
	s.fresh.mu.Lock()
	if time.Since(s.fresh.lastCheck) < s.checkTTL() {
		s.fresh.mu.Unlock()
		return
	}
	s.fresh.lastCheck = time.Now()
	lastFP := s.fresh.lastFP
	s.fresh.mu.Unlock()

	fp, err := s.fingerprint()
	if err != nil {
		return // not a repo (or unreadable): silent, like --git-aware builds
	}
	if fp == lastFP {
		return
	}
	s.fresh.mu.Lock()
	s.fresh.lastFP = fp
	s.fresh.mu.Unlock()

	if s.fresh.inFlight.Swap(true) {
		return // singleflight: a rebuild is already running
	}
	go func() {
		defer s.fresh.inFlight.Store(false)
		time.Sleep(s.debounce())
		if err := s.refresh.Rebuild(); err != nil {
			fmt.Fprintln(os.Stderr, "warning: auto-rebuild failed:", err)
			return
		}
		reload := s.refresh.Reload
		if reload == nil {
			reload = func() (*ir.Graph, error) { return exporter.LoadGraph(s.refresh.GraphFile) }
		}
		g, err := reload()
		if err != nil {
			fmt.Fprintln(os.Stderr, "warning: auto-reload failed:", err)
			return
		}
		s.swapGraph(g)
		fmt.Fprintln(os.Stderr, "auto-rebuild: graph refreshed")
	}()
}
