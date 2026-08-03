// Package graphed exposes the public Build API that orchestrates
// the full pipeline: scan -> parse -> analyze -> export.
package graphed

import (
	"context"
	"fmt"
	"runtime"
	"sync"

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
	var docs []ir.Document
	var graph ir.Graph
	var dbPath string
	var store *vector_store.BadgerStore

	ctx := context.Background()

	// Stage 1: discover every file under the root directory, honoring
	// .gitignore files and any user-supplied Exclude patterns.
	fs := scanner.FileSystemScanner{
		Exclude:     opts.Exclude,
		IgnoreFiles: opts.IgnoreFiles,
		NoGitIgnore: opts.NoGitIgnore,
	}
	files, err = fs.Scan(opts.Root)
	if err != nil {
		return err
	}
	fmt.Printf("[DEBUG] 1. Scanner found %d files\n", len(files))

	// Stage 2: parse each file into an enriched Document. Parsing is
	// embarrassingly parallel -- each file is independent -- so we fan it
	// out across a worker pool sized by opts.Jobs. The analyzer (Stage 3)
	// stays serial for deterministic linking.
	jobs := opts.Jobs
	if jobs <= 0 {
		jobs = runtime.NumCPU()
	}

	docs = make([]ir.Document, 0, len(files))
	var mu sync.Mutex
	var firstErr error
	filesCh := make(chan scanner.File)
	var wg sync.WaitGroup

	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range filesCh {
				doc, err := parser.Parse(file)
				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = fmt.Errorf("builder error %w", err)
				}
				if err == nil {
					docs = append(docs, doc)
				}
				mu.Unlock()
			}
		}()
	}

	for _, file := range files {
		filesCh <- file
	}
	close(filesCh)
	wg.Wait()

	if firstErr != nil {
		return firstErr
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

		// Cross-check the configured Dimensions against what the model
		// actually produces. 0 means "auto" (read from the model); a
		// mismatch protects us from loading a store built with a
		// different model width.
		if opts.Dimensions > 0 {
			probe, err := embedder.EmbedText(ctx, "dimension probe")
			if err != nil {
				return fmt.Errorf("failed to probe embedding dimensions: %w", err)
			}
			if len(probe) != opts.Dimensions {
				return fmt.Errorf("embedding dimension mismatch: model produces %d dims, got --dimensions=%d", len(probe), opts.Dimensions)
			}
		}

		// Loop over the analyzed graph nodes to populate goembedx
		for path, docNode := range graph.Documents {
			// 1. Embed the Document node as a high-level context block
			docPayload := fmt.Sprintf("File: %s. Language: %s. Summary of contents.", path, docNode.Format)
			docVec, err := embedder.EmbedText(ctx, docPayload)
			if err != nil {
				return fmt.Errorf("failed embedding document %s: %w", path, err)
			}

			if err = engine.Add(docNode.Path, docVec); err != nil {
				return fmt.Errorf("failed to store document vector for %s: %w", docNode.Path, err)
			}

			// 2. Embed individual structural AST Entities inside this file
			for _, entity := range docNode.Entities {
				entityPayload := fmt.Sprintf("Type: %s, Name: %s. Defined in %s.", entity.Type, entity.Name, path)
				entityVec, err := embedder.EmbedText(ctx, entityPayload)
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
