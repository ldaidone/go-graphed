// Package graphed exposes the public Build API that orchestrates
// the full pipeline: scan -> parse -> analyze -> export.
package graphed

import (
	"context"
	"fmt"
	"github.com/ldaidone/go-graphed/internal/analyzer"
	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/ldaidone/go-graphed/internal/exporter"
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/parser"
	"github.com/ldaidone/go-graphed/internal/scanner"
	"github.com/ldaidone/go-graphed/internal/utils/vector_store"
	"github.com/ldaidone/goembedx/pkg/embedx"
)

func Build(opts BuildOptions) error {
	var err error
	var files []scanner.File
	var doc ir.Document
	var docs []ir.Document
	var graph ir.Graph
	var fs scanner.FileSystemScanner
	var docVec []float32
	var entityVec []float32
	var dbPath string
	var store *vector_store.BadgerStore

	ctx := context.Background()

	// Stage 1: discover every file under the root directory.
	files, err = fs.Scan(opts.Root)
	if err != nil {
		return err
	}
	fmt.Printf("[DEBUG] 1. Scanner found %d files\n", len(files))

	// Stage 2: parse each file into an enriched Document.
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

	// Stage 3.5: Initialize Pure-Go Native Vector Embedding Pipeline
	// Only run if a local model path has been provided
	if opts.ModelPath != "" {
		dbPath, err = config.BadgerDBPath(opts.DBRoot)
		if err != nil {
			return fmt.Errorf("failed to determine database path: %w", err)
		}

		store, err = vector_store.NewBadgerStore(dbPath)
		if err != nil {
			return fmt.Errorf("failed to initialize vector store: %w", err)
		}
		// Close the underlying Badger database when the build completes
		defer store.Close()

		engine := embedx.New(store)

		fmt.Println("[DEBUG] 4.5. Initializing Pure-Go Native Embedder and indexing vectors...")

		embedder, err := analyzer.NewNativeEmbedder(ctx, opts.ModelPath)
		if err != nil {
			return fmt.Errorf("failed to load native semantic embedding engine: %w", err)
		}
		defer embedder.Close(ctx)

		// Loop over the analyzed graph nodes to populate goembedx
		for path, docNode := range graph.Documents {
			// 1. Embed the Document node as a high-level context block
			docPayload := fmt.Sprintf("File: %s. Language: %s. Summary of contents.", path, docNode.Format)
			docVec, err = embedder.EmbedText(ctx, docPayload)
			if err != nil {
				return fmt.Errorf("failed embedding document %s: %w", path, err)
			}

			if err = engine.Add(docNode.Path, docVec); err != nil {
				return fmt.Errorf("failed to store document vector for %s: %w", docNode.Path, err)
			}

			// 2. Embed individual structural AST Entities inside this file
			for _, entity := range docNode.Entities {
				entityPayload := fmt.Sprintf("Type: %s, Name: %s. Defined in %s.", entity.Type, entity.Name, path)
				entityVec, err = embedder.EmbedText(ctx, entityPayload)
				if err != nil {
					return fmt.Errorf("failed embedding entity %s: %w", entity.ID, err)
				}

				if err = engine.Add(entity.ID, entityVec); err != nil {
					return fmt.Errorf("failed to store entity vector for %s: %w", entity.ID, err)
				}
			}
		}
		fmt.Println("[DEBUG] 4.6. Vector indexing complete.")
	}

	// Stage 4: persist the graph structure to disk.
	err = exporter.JSON(graph, opts.Output)
	if err != nil {
		return fmt.Errorf("builder error %w", err)
	}
	return nil
}
