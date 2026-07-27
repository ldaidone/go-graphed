// Package scanner walks a directory tree and hands back a list of
// files enriched with language metadata.  The interface lives in
// its own file so alternative implementations (e.g. a git-aware
// scanner that skips ignored files) can be swapped in without
// changing any downstream code.
package scanner

// Scanner abstracts the filesystem walk so the rest of the pipeline
// is never coupled to a concrete traversal strategy.
type Scanner interface {
	Scan(root string) ([]File, error)
}
