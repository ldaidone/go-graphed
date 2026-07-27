// Package exporter serialises the IR Graph into a concrete output
// format.  JSON is the only format today but the package structure
// lets other formats (GraphML, RDF, Cypher) be added as separate
// files without touching the pipeline.
package exporter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ldaidone/go-graphed/internal/ir"
)

// JSON writes the graph to a pretty-printed JSON file.
// Creating the output directory here (rather than in the caller)
// means every caller gets the same guarantee: if JSON() succeeds,
// the file exists on disk.
func JSON(graph ir.Graph, output string) error {
	// Ensure the output directory exists -- users may pass a
	// nested path like "out/graphs/result.json" without pre-creating it.
	dir := filepath.Dir(output)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// 4-space indent produces human-readable output that is easy
	// to diff in version control.
	data, err := json.MarshalIndent(graph, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal IR graph to JSON: %w", err)
	}

	if err := os.WriteFile(output, data, 0644); err != nil {
		return fmt.Errorf("failed to write JSON file to %s: %w", output, err)
	}

	return nil
}
