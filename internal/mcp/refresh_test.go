package mcp

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ldaidone/go-graphed/internal/ir"
)

func TestMaybeAutoRebuild_DisabledByDefault(t *testing.T) {
	s := &Server{}       // zero value: no auto-rebuild, no root, no rebuild func
	s.maybeAutoRebuild() // must not panic, block, or spawn anything
	s.maybeAutoRebuild()
}

func TestMaybeAutoRebuild_NonRepoDegradesSilently(t *testing.T) {
	var calls atomic.Int32
	s := &Server{
		autoRebuild: true,
		refresh: RefreshConfig{
			Root:    t.TempDir(), // not a repo
			Rebuild: func() error { calls.Add(1); return nil },
		},
		refreshDebounce: 10 * time.Millisecond,
	}
	s.maybeAutoRebuild()
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 0 {
		t.Error("non-repo must never trigger a rebuild")
	}
}

func TestMaybeAutoRebuild_TriggersOnceAndSwapsGraph(t *testing.T) {
	fps := []string{"b", "b", "b"}
	var fpCalls atomic.Int32
	fresh := &ir.Graph{Documents: map[string]*ir.Document{}}
	s := &Server{
		graph:       &ir.Graph{Documents: map[string]*ir.Document{}},
		autoRebuild: true,
		refresh: RefreshConfig{
			Root: "x",
			Fingerprint: func(root string) (string, error) {
				fpCalls.Add(1)
				return fps[0], nil
			},
			Rebuild: func() error { return nil },
			Reload:  func() (*ir.Graph, error) { return fresh, nil },
		},
		refreshTTL:      time.Hour, // fingerprint once; motion comes from lastFP
		refreshDebounce: 10 * time.Millisecond,
	}
	s.fresh.lastFP = "a" // simulate startup state; tree moved to "b"

	var rebuilds atomic.Int32
	s.refresh.Rebuild = func() error { rebuilds.Add(1); return nil }

	s.maybeAutoRebuild() // triggers (b != a)
	s.maybeAutoRebuild() // TTL-cached: no second fingerprint, rebuild in flight anyway
	time.Sleep(200 * time.Millisecond)

	if fpCalls.Load() != 1 {
		t.Errorf("fingerprint calls = %d, want 1 (TTL-cached)", fpCalls.Load())
	}
	if rebuilds.Load() != 1 {
		t.Errorf("rebuilds = %d, want 1 (singleflight)", rebuilds.Load())
	}
	if s.snapshot() != fresh {
		t.Error("graph was not swapped after rebuild + reload")
	}
}

func TestMaybeAutoRebuild_RebuildErrorKeepsServing(t *testing.T) {
	old := &ir.Graph{Documents: map[string]*ir.Document{}}
	s := &Server{
		graph:       old,
		autoRebuild: true,
		refresh: RefreshConfig{
			Root:        "x",
			Fingerprint: func(root string) (string, error) { return "b", nil },
			Rebuild:     func() error { return errors.New("boom") },
		},
		refreshDebounce: 10 * time.Millisecond,
	}
	s.fresh.lastFP = "a"
	s.maybeAutoRebuild()
	time.Sleep(200 * time.Millisecond)
	if s.snapshot() != old {
		t.Error("failed rebuild must keep serving the old snapshot")
	}
}
