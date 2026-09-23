// Package graphed exposes the public Build API that orchestrates
// the full pipeline: scan -> parse -> analyze -> export.
package graphed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/ldaidone/go-graphed/internal/analyzer"
	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/ldaidone/go-graphed/internal/exporter"
	"github.com/ldaidone/go-graphed/internal/git"
	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/parser"
	"github.com/ldaidone/go-graphed/internal/scanner"
	"github.com/ldaidone/goembedx/pkg/store"
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
	var vstore *store.SQLite

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
	// Never index the tool's own output. The default `kg build .`
	// writes graph.json inside the scanned root, so without this the
	// scanner picks up a stale graph.json as a JSON document on every
	// rebuild. Filtering is by absolute path so relative/absolute
	// spellings of Root and Output still match.
	files = excludeOutputFile(files, opts.Output)
	verbosef(opts.Verbose, "1. Scanner found %d files\n", len(files))

	// git-aware filtering. ChangedOnly restricts the
	// scan to working-tree-changed files; it requires a git repo.
	if opts.ChangedOnly || opts.GitAware {
		var gitErr error
		files, gitErr = applyGitScope(files, opts.Root, opts.ChangedOnly)
		if gitErr != nil {
			if opts.ChangedOnly {
				return gitErr
			}
			fmt.Fprintln(os.Stderr, "warning: git-aware build continuing without git:", gitErr)
		} else {
			verbosef(opts.Verbose, "1.5. Git scope: %d files\n", len(files))
		}
	}

	// Parse each file into an enriched Document. Parsing is  embarrassingly parallel -- each file is independent --
	// so we fan it out across a worker pool sized by opts.Jobs. The analyzer stays serial for deterministic linking.
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
		wg.Go(func() {
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
		})
	}

	for _, file := range files {
		filesCh <- file
	}
	close(filesCh)
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	verbosef(opts.Verbose, "3. Total documents parsed: %d\n", len(docs))

	// Assemble documents into a graph and infer links.
	graph, err = analyzer.Build(docs)
	if err != nil {
		return fmt.Errorf("builder error %w", err)
	}
	// git metadata + co-change links. Failures degrade
	// to a warning so a broken repo never blocks a code graph build.
	if opts.ChangedOnly || opts.GitAware {
		if gerr := enrichGraphWithGit(&graph, opts.Root, opts.GitLimit); gerr != nil {
			fmt.Fprintln(os.Stderr, "warning: git enrichment skipped:", gerr)
		} else {
			verbosef(opts.Verbose, "3b. Git enrichment applied (%d links)\n", len(graph.Links))
		}
	}
	verbosef(opts.Verbose, "4. Graph documents mapped: %d\n", len(graph.Documents))

	// Initialize Pure-Go Native Vector Embedding Pipeline
	// Only run if a local model path has been provided
	if opts.ModelPath != "" {
		dbPath, err = config.BadgerDBPath(opts.DBRoot)
		if err != nil {
			return fmt.Errorf("failed to determine database path: %w", err)
		}

		vstore, err = store.NewSQLite(dbPath)
		if err != nil {
			return fmt.Errorf("failed to initialize vector store: %w", err)
		}
		// Close the underlying SQLite database when the build completes
		defer vstore.Close()

		verbosef(opts.Verbose, "4.5. Initializing Pure-Go Native Embedder and indexing vectors...\n")

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

		// Embed every document and entity, skipping nodes whose
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

			//    Document vector: a high-level context block anchored in real
			//    file content. When the file is readable, the first bytes of
			//    the body (package clause, doc comment) back the stub so
			//    semantic search can match on actual content, not just paths.
			docPayload := fmt.Sprintf("File: %s. Language: %s. Summary of contents.", path, docNode.Format)
			if readErr == nil {
				docPayload = fmt.Sprintf("File: %s. Language: %s.\n%s", path, docNode.Format, truncateBytes(string(body), docPayloadBodyMax))
			}
			jobs = append(jobs, embedJob{ID: docNode.Path, Payload: docPayload})

			//    Entity vectors: structural AST entities embed the exact
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
			vec, _, meta, err := vstore.Get(job.ID)
			if err == nil && meta != nil {
				if existing, ok := meta["payload_hash"].(string); ok && existing == hash && len(vec) > 0 {
					reused++
					continue
				}
			}
			dirty = append(dirty, pendingJob{job: job, hash: hash})
		}
		verbosef(opts.Verbose, "4.55. Embedding %d nodes (%d unchanged).\n", len(dirty), reused)

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
			if err := vstore.Add(dirty[i].job.ID, vectors[i], meta); err != nil {
				return fmt.Errorf("failed to store vector for %s: %w", dirty[i].job.ID, err)
			}
		}

		// Drop vectors whose node no longer exists (file or entity removed),
		// so stale IDs never leak into semantic search results.
		pruned, err := pruneStaleVectors(vstore, jobs)
		if err != nil {
			return fmt.Errorf("failed to prune stale vectors: %w", err)
		}
		verbosef(opts.Verbose, "4.6. Vector indexing complete (%d vectors, %d reused, %d pruned).\n", len(jobs), reused, pruned)
	}

	// Persist the graph structure to disk.
	err = exporter.JSON(graph, opts.Output)
	if err != nil {
		return fmt.Errorf("builder error %w", err)
	}
	return nil
}

// excludeOutputFile drops the scan entry that points at the graph output
// file so `kg build .` never indexes its own prior output.
func excludeOutputFile(files []scanner.File, output string) []scanner.File {
	if output == "" {
		return files
	}
	absOut, err := filepath.Abs(output)
	if err != nil {
		return files
	}
	kept := files[:0]
	for _, f := range files {
		abs, err := filepath.Abs(f.Path)
		if err != nil {
			kept = append(kept, f)
			continue
		}
		if abs == absOut {
			continue
		}
		kept = append(kept, f)
	}
	return kept
}

// verbosef mirrors fmt.Fprintf to stderr only when verbose is enabled,
// keeping default build output quiet for scripts and CI.
func verbosef(verbose bool, format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

// applyGitScope optionally restricts files to working-tree-changed paths.
// changedOnly=false is a no-op returning files unchanged. Status lookup
// failures are returned so callers can decide between warn and fail.
func applyGitScope(files []scanner.File, root string, changedOnly bool) ([]scanner.File, error) {
	if !changedOnly {
		return files, nil
	}
	status, err := git.Status(root)
	if err != nil {
		return files, err
	}
	kept := make([]scanner.File, 0, len(files))
	for _, f := range files {
		abs, aerr := filepath.Abs(f.Path)
		if aerr != nil {
			kept = append(kept, f)
			continue
		}
		if _, ok := status[abs]; ok {
			kept = append(kept, f)
		}
	}
	return kept, nil
}

// enrichGraphWithGit stamps per-document git metadata and appends
// `co_changed` links between files touched by the same recent commits.
// Missing repos or histories are errors for the caller to downgrade.
func enrichGraphWithGit(graph *ir.Graph, root string, limit int) error {
	status, err := git.Status(root)
	if err != nil {
		return err
	}
	head, herr := git.Head(root)
	if herr != nil {
		// Status worked but HEAD is unborn (no commits yet): still stamp
		// status, skip co-change links.
		stampGitStatus(graph, status, "", "", "")
		return nil
	}
	stampGitStatus(graph, status, head.Hash, head.Author, head.Date.Format("2006-01-02 15:04:05"))
	commits, err := git.Recent(root, limit)
	if err != nil {
		return err
	}
	graph.Links = append(graph.Links, coChangeLinks(graph, root, commits)...)
	return nil
}

// stampGitStatus records working-tree status plus HEAD identity on every
// document so MCP consumers can answer "what changed" without git.
func stampGitStatus(graph *ir.Graph, status map[string]string, hash, author, date string) {
	for path, doc := range graph.Documents {
		if doc.Metadata == nil {
			doc.Metadata = map[string]string{}
		}
		abs, err := filepath.Abs(path)
		code := "clean"
		if err == nil {
			if c, ok := status[abs]; ok {
				code = c
			}
		}
		doc.Metadata["git_status"] = code
		if hash != "" {
			doc.Metadata["git_head"] = hash
			doc.Metadata["git_head_author"] = author
			doc.Metadata["git_head_date"] = date
		}
	}
}

// coChangeLinks emits one deduplicated inferred edge per file pair that
// appears together in a recent commit. Pairs in oversized commits are
// skipped to bound the edge count.
func coChangeLinks(graph *ir.Graph, root string, commits []git.CommitInfo) []ir.Link {
	_, repoRoot, err := git.Open(root)
	if err != nil {
		repoRoot = root
	}
	absRoot, _ := filepath.Abs(repoRoot)
	// Map absolute path back to the document key used in graph.Documents.
	byAbs := make(map[string]string, len(graph.Documents))
	for key := range graph.Documents {
		abs, aerr := filepath.Abs(key)
		if aerr == nil {
			byAbs[abs] = key
		}
	}
	seen := map[string]int{}
	for _, c := range commits {
		var present []string
		for _, rel := range c.Files {
			abs := rel
			if !filepath.IsAbs(rel) {
				base := absRoot
				if base == "" {
					var aerr error
					base, aerr = filepath.Abs(root)
					if aerr != nil {
						continue
					}
				}
				abs = filepath.Join(base, filepath.FromSlash(rel))
			}
			if key, ok := byAbs[abs]; ok {
				present = append(present, key)
			}
		}
		if len(present) < 2 || len(present) > 20 {
			continue
		}
		for i := 0; i < len(present); i++ {
			for j := i + 1; j < len(present); j++ {
				a, b := present[i], present[j]
				if a > b {
					a, b = b, a
				}
				seen[a+"\x00"+b]++
			}
		}
	}
	links := make([]ir.Link, 0, len(seen))
	for k, n := range seen {
		idx := strings.Index(k, "\x00")
		w := 0.3 + 0.1*float64(n)
		if w > 1.0 {
			w = 1.0
		}
		links = append(links, ir.Link{
			SourceID:   k[:idx],
			TargetID:   k[idx+1:],
			Type:       "co_changed",
			Weight:     w,
			SourceType: ir.LinkSourceInferred,
		})
	}
	return links
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
func pruneStaleVectors(vstore *store.SQLite, jobs []embedJob) (int, error) {
	valid := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		valid[job.ID] = struct{}{}
	}
	return vstore.DeleteStale(valid)
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
