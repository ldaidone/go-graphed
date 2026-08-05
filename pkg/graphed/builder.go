// Package graphed exposes the public Build API that orchestrates
// the full pipeline: scan -> parse -> analyze -> export.
package graphed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/ldaidone/go-graphed/internal/analyzer"
	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/ldaidone/go-graphed/internal/exporter"
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/parser"
	"github.com/ldaidone/go-graphed/internal/scanner"
	"github.com/ldaidone/go-graphed/internal/utils/vector_store"
)

// DefaultSkipEmbedTypes are entity types that do not produce embeddings by
// default. They duplicate structural information already captured by the
// graph — import/package/link are graph edges, config data (property, array,
// object, ...) is tree structure, and plain variable assignments rarely
// answer a semantic query — while costing a full model pass each. Skipping
// them cuts the embedding volume of a large project by a large factor with
// little retrieval loss; functions, methods, types, headings and tables still
// embed. Override via BuildOptions.SkipEmbedTypes.
var DefaultSkipEmbedTypes = []string{
	"import", "package",
	"link", "image-asset", "page",
	"property", "sequence-item", "container", "array", "object", "data-table",
	"variable", "export",
}

// skipEmbedTypes builds the set of entity types that must not be embedded.
func skipEmbedTypes(types []string) map[string]bool {
	skip := map[string]bool{"import": true, "package": true}
	if types == nil {
		types = DefaultSkipEmbedTypes
	}
	for _, t := range types {
		if t = strings.TrimSpace(t); t != "" {
			skip[t] = true
		}
	}
	return skip
}

// Build implements the full scan -> parse -> analyze -> export pipeline,
// optionally indexing semantic embeddings for every document and entity when a
// model is configured.
func Build(opts BuildOptions) error {
	var err error
	var files []scanner.File
	var docs []ir.Document
	var graph ir.Graph
	var dbPath string
	var store vector_store.Store

	ctx := context.Background()

	// Stage 1: discover every file under the root directory, honoring
	// .gitignore files and any user-supplied Exclude patterns.
	fs := scanner.FileSystemScanner{
		Exclude:       opts.Exclude,
		IgnoreFiles:   opts.IgnoreFiles,
		NoGitIgnore:   opts.NoGitIgnore,
		NoDefaultSkip: opts.NoDefaultSkip,
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

		store, err = vector_store.NewSQLiteStore(dbPath)
		if err != nil {
			return fmt.Errorf("failed to initialize vector store: %w", err)
		}
		// Close the underlying Badger database when the build completes
		defer store.Close()

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

		// Stage 3.5: embed every document and entity, skipping nodes whose
		// payload hash is unchanged since the last build. Rebuilds are then
		// incremental: only new or modified nodes pay the model cost, so a
		// typical edit lands in seconds instead of minutes.
		// Payloads are bounded: the gte model truncates input at 512 tokens
		// (~1.6 KB) and embedding cost is linear in payload length, so feeding
		// it whole files is pure waste (a 2 KB and a 100 KB payload produce
		// identical vectors). Bounded, content-bearing payloads keep semantic
		// retrieval working while the batch runs concurrently across workers.
		skip := skipEmbedTypes(opts.SkipEmbedTypes)
		jobs := make([]embedJob, 0, len(graph.Documents)*2)
		for path, docNode := range graph.Documents {
			body, readErr := os.ReadFile(path)
			var lines []string
			if readErr == nil {
				lines = strings.Split(string(body), "\n")
			}

			// 1. Document vector: a high-level context block anchored in real
			//    file content. When the file is readable, the first bytes of
			//    the body (package clause, doc comment) back the stub so
			//    semantic search can match on actual content, not just paths.
			docPayload := fmt.Sprintf("File: %s. Language: %s. Summary of contents.", path, docNode.Format)
			if readErr == nil {
				docPayload = fmt.Sprintf("File: %s. Language: %s.\n%s", path, docNode.Format, truncateBytes(string(body), docPayloadBodyMax))
			}
			jobs = append(jobs, embedJob{ID: docNode.Path, Payload: docPayload})

			// 2. Entity vectors: structural AST entities embed the exact
			//    source slice they cover, bounded so a huge function cannot
			//    hog the batch. Unannotated entities fall back to a type/name
			//    stub. Noisy/structural types are skipped to keep the batch
			//    small on large projects.
			for _, entity := range docNode.Entities {
				if skip[entity.Type] {
					continue
				}
				entityPayload := fmt.Sprintf("Type: %s, Name: %s. Defined in %s.", entity.Type, entity.Name, path)
				if readErr == nil {
					if snippet := entitySnippet(lines, entity); snippet != "" {
						entityPayload = fmt.Sprintf("Type: %s, Name: %s. Defined in %s.\n%s", entity.Type, entity.Name, path, snippet)
					}
				}
				jobs = append(jobs, embedJob{ID: entity.ID, Payload: entityPayload})
			}
		}

		// Split jobs into dirty (must embed) and clean (vector already stored
		// for this exact payload). The stored payload hash travels with each
		// vector's metadata so the comparison is a single map lookup.
		type pendingJob struct {
			job  embedJob
			hash string
		}
		var dirty []pendingJob
		reused := 0
		for _, job := range jobs {
			hash := payloadHash(job.Payload)
			vec, _, meta, err := store.Get(job.ID)
			if err == nil && meta != nil {
				if existing, ok := meta["payload_hash"].(string); ok && existing == hash && len(vec) > 0 {
					reused++
					continue
				}
			}
			dirty = append(dirty, pendingJob{job: job, hash: hash})
		}
		fmt.Printf("[DEBUG] 4.55. Embedding %d nodes (%d unchanged).\n", len(dirty), reused)

		payloads := make([]string, len(dirty))
		for i := range dirty {
			payloads[i] = dirty[i].job.Payload
		}
		vectors, err := embedder.EmbedTexts(ctx, payloads, embedWorkers(runtime.NumCPU()))
		if err != nil {
			return fmt.Errorf("failed embedding graph nodes: %w", err)
		}
		for i := range dirty {
			meta := map[string]any{"payload_hash": dirty[i].hash}
			if err := store.Add(dirty[i].job.ID, vectors[i], meta); err != nil {
				return fmt.Errorf("failed to store vector for %s: %w", dirty[i].job.ID, err)
			}
		}

		// Drop vectors whose node no longer exists (file or entity removed),
		// so stale IDs never leak into semantic search results.
		pruned, err := pruneStaleVectors(store, jobs)
		if err != nil {
			return fmt.Errorf("failed to prune stale vectors: %w", err)
		}
		fmt.Printf("[DEBUG] 4.6. Vector indexing complete (%d vectors, %d reused, %d pruned).\n", len(jobs), reused, pruned)
	}

	// Stage 4: persist the graph structure to disk.
	err = exporter.JSON(graph, opts.Output)
	if err != nil {
		return fmt.Errorf("builder error %w", err)
	}
	return nil
}

// Payload bounds. The gte model truncates input at 512 tokens (~1.6 KB) and
// embedding cost is linear in payload length, so these caps trade a small
// amount of redundant tail text for a large speedup on big files.
const (
	// docPayloadBodyMax caps how much of a file body feeds the document vector.
	docPayloadBodyMax = 256

	// entitySnippetMax caps the source slice embedded for a structural entity.
	entitySnippetMax = 200

	// embedWorkersMax caps concurrent embedding workers. Past ~8 workers the
	// SIMD kernels saturate memory bandwidth and extra goroutines only add
	// per-worker transformer-buffer allocations.
	embedWorkersMax = 8
)

// embedJob pairs a graph node ID with the text payload used to build its vector.
type embedJob struct {
	ID      string
	Payload string
}

// embedWorkers clamps the requested worker count into a sane range.
func embedWorkers(n int) int {
	if n <= 0 {
		n = runtime.NumCPU()
	}
	if n > embedWorkersMax {
		n = embedWorkersMax
	}
	return n
}

// truncateBytes returns at most n bytes of s.
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// payloadHash fingerprints the exact text a vector was derived from. It is
// stored alongside the vector so rebuilds can skip re-embedding unchanged
// nodes without re-running the model.
func payloadHash(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// pruneStaleVectors removes every stored vector whose node ID is no longer
// part of the freshly built graph, so deleted files and entities stop
// surfacing in semantic search results.
func pruneStaleVectors(store vector_store.Store, jobs []embedJob) (int, error) {
	valid := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		valid[job.ID] = struct{}{}
	}
	return store.DeleteStale(valid)
}

// entitySnippet slices a file's lines covering an entity's start_line/end_line
// metadata range, bounded to entitySnippetMax bytes, and returns "" when the
// entity carries no line info.
func entitySnippet(lines []string, entity ir.Entity) string {
	start, err := strconv.Atoi(entity.Metadata["start_line"])
	if err != nil || start <= 0 {
		return ""
	}
	end, err := strconv.Atoi(entity.Metadata["end_line"])
	if err != nil || end < start {
		end = start
	}
	if start > len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	snippet := strings.Join(lines[start-1:end], "\n")
	return truncateBytes(snippet, entitySnippetMax)
}
