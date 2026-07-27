// Package graphed exposes the public Build API that orchestrates
// the full pipeline: scan -> parse -> analyze -> export.
// Internal packages are not imported by external consumers, so
// this package acts as the single façade over the entire system.
package graphed

import (
	"fmt"

	"github.com/ldaidone/go-graphed/internal/analyzer"
	"github.com/ldaidone/go-graphed/internal/exporter"
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/parser"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

// Build runs the complete knowledge-graph pipeline for the given
// options.  Each stage is called sequentially because later stages
// depend on earlier ones -- parallelism could be introduced inside
// the parser loop (per-file) without changing this ordering.
func Build(opts BuildOptions) error {
	var err error
	var files []scanner.File
	var doc ir.Document
	var docs []ir.Document
	var graph ir.Graph
	var fs scanner.FileSystemScanner

	// Stage 1: discover every file under the root directory.
	files, err = fs.Scan(opts.Root)
	if err != nil {
		return err
	}
	fmt.Printf("[DEBUG] 1. Scanner found %d files\n", len(files))

	// Stage 2: parse each file into an enriched Document.
	// Errors are fatal here because a single corrupt file would
	// produce an incomplete graph -- better to fail early.
	for _, file := range files {
		fmt.Printf("[DEBUG] 2. Processing file: %s (Lang: %s)\n", file.Path, file.Language)
		doc, err = parser.Parse(file)
		if err != nil {
			return fmt.Errorf("builder error %w", err)
		}
		docs = append(docs, doc)
	}
	fmt.Printf("[DEBUG] 3. Total documents parsed: %d\n", len(docs))

	// Stage 3: assemble documents into a graph and infer links.
	graph, err = analyzer.Build(docs)
	if err != nil {
		return fmt.Errorf("builder error %w", err)
	}

	fmt.Printf("[DEBUG] 4. Graph documents mapped: %d\n", len(graph.Documents))

	// Stage 4: persist the graph to disk.
	err = exporter.JSON(graph, opts.Output)
	if err != nil {
		return fmt.Errorf("builder error %w", err)
	}
	return nil
}
