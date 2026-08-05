package vector_store

import (
	"github.com/ldaidone/goembedx/pkg/embedx"
)

// Store is the persistence contract shared by the build pipeline (writer)
// and the MCP server (reader). It extends the embedx.Store contract with the
// plain-vector and pruning operations the builder needs. Callers should
// depend on this interface rather than a concrete backend so the storage
// engine can be swapped without touching them.
type Store interface {
	embedx.VectorStore
	embedx.Store
	// DeleteStale removes every stored vector whose key is absent from valid,
	// returning how many were deleted.
	DeleteStale(valid map[string]struct{}) (int, error)
}

// Compile-time checks that every backend satisfies the shared contract.
var (
	_ Store = (*BadgerStore)(nil)
	_ Store = (*SQLiteStore)(nil)
)
