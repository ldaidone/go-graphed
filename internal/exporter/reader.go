package exporter

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ldaidone/go-graphed/internal/ir"
	"os"
)

var (
	// ErrEmptyGraphFilePath reports that no graph file path was provided.
	ErrEmptyGraphFilePath = errors.New("graph file path cannot be empty")

	// ErrCorruptGraphSchema reports that a loaded file does not carry a valid
	// knowledge graph schema.
	ErrCorruptGraphSchema = errors.New("loaded file does not contain a valid code knowledge graph schema")
)

// LoadGraph reads, streams, and validates a generated JSON graph file into the IR memory model.
// Version policy (lenient-warn): missing schema_version (pre-v1.0) and
// newer-than-current versions load with a stderr warning; only
// structurally corrupt graphs are rejected.
func LoadGraph(path string) (*ir.Graph, error) {
	if path == "" {
		return nil, ErrEmptyGraphFilePath
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open graph storage asset: %w", err)
	}
	// Deferred execution handles cleanup even if JSON stream processing panics
	defer file.Close()

	// Wrap the file reader in a buffered stream buffer (4KB-32KB chunks default).
	// This dramatically reduces system call overhead when processing large code graphs.
	bufferedReader := bufio.NewReader(file)

	var graph ir.Graph
	decoder := json.NewDecoder(bufferedReader)

	// Disallow unknown fields to catch breaking schema mutations early during development
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&graph); err != nil {
		return nil, fmt.Errorf("malformed graph structural payload: %w", err)
	}

	// Lenient version gate: warn, never fail, on stale or future schemas.
	switch {
	case graph.SchemaVersion == 0:
		fmt.Fprintln(os.Stderr, "warning: graph.json has no schema_version (pre-v1.0 snapshot); reading leniently, rebuild with `kg build` to upgrade")
	case graph.SchemaVersion > ir.CurrentSchemaVersion:
		fmt.Fprintf(os.Stderr, "warning: graph.json schema_version=%d is newer than supported v%d; reading leniently\n", graph.SchemaVersion, ir.CurrentSchemaVersion)
	}

	// Validate structural contract constraints
	if err := validateGraphStructure(&graph); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorruptGraphSchema, err)
	}

	return &graph, nil
}

// validateGraphStructure inspects internal runtime reference matrices to avoid downstream nil pointer mutations.
func validateGraphStructure(g *ir.Graph) error {
	if g == nil {
		return errors.New("graph target instance is nil")
	}

	// Safety check: Initialize maps if they were completely absent in the JSON to avoid runtime map panics
	if g.Documents == nil {
		return errors.New("missing required top-level 'nodes' dictionary definition")
	}
	if g.Links == nil {
		return errors.New("missing required top-level 'edges' relation collection")
	}

	// Perform basic schema health validation
	// (Adjust fields based on what your actual ir.Graph struct defines in internal/ir/types.go)
	if len(g.Documents) == 0 {
		return errors.New("graph storage asset contains 0 functional metadata nodes")
	}

	return nil
}
