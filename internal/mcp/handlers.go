package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/goembedx/pkg/embedx"
	mcp_golang "github.com/metoro-io/mcp-golang"
)

// --- Tool Argument Schemas ---

type DocumentQueryArgs struct {
	Path string `json:"path" jsonschema:"required,description=The explicit file path identifier of the target document (e.g., internal/analyzer/analyzer.go)"`
}

type FilterQueryArgs struct {
	Format string `json:"format" jsonschema:"required,description=Filter constraint by file format (e.g., golang, markdown)"`
}

type EntityFilterArgs struct {
	Type string `json:"type" jsonschema:"required,description=The structural entity type to scan for (e.g., struct, interface, heading)"`
}

type ClusterListArgs struct {
	Kind string `json:"kind" jsonschema:"description=Filter clusters by derivation kind: directory, module, or network. Empty returns every cluster."`
}

type ClusterDetailArgs struct {
	ID string `json:"id" jsonschema:"required,description=The unique cluster identifier (e.g., directory:internal/ir)"`
}

type MetricsArgs struct {
	MaxResults int `json:"maxResults" jsonschema:"description=Maximum number of ranked documents to return. 0 means every ranked document."`
}

type NarrowContextArgs struct {
	EntryPath   string   `json:"entryPath" jsonschema:"required,description=The file path where the bug or feature investigation starts."`
	SearchQuery string   `json:"searchQuery" jsonschema:"required,description=The semantic intent or feature description to slice context against (e.g. error handling in DB routines)."`
	MaxHops     int      `json:"maxHops" jsonschema:"description=Degrees of structural topology separation to traverse. Defaults to 1."`
	MinScore    float32  `json:"minScore" jsonschema:"description=Minimum semantic similarity score bounds between 0.0 and 1.0. Defaults to 0.65."`
	SourceType  string   `json:"sourceType" jsonschema:"description=Restrict topology traversal to links of this provenance: 'extracted' (parsed directly from source) or 'inferred' (derived by heuristics). Empty means all links."`
	Exclude     []string `json:"exclude" jsonschema:"description=File path patterns to exclude from the returned context. Each entry may be a path prefix (e.g. \"vendor\") or a glob (e.g. \"*.generated.go\")."`
	MaxTokens   int      `json:"maxTokens" jsonschema:"description=Approximate token budget for the returned context. Snippets are prioritized by semantic score and truncated to stay under this cap. 0 means unlimited."`
	Format      string   `json:"format" jsonschema:"description=Response format: \"markdown\" (default) or \"json\"."`
}

// minSemanticSearchResults is the floor for the vector-search candidate
// pool in handleGetNarrowedContext. It is applied because the candidate
// count used to be derived purely from the topological frontier, and a
// graph with few or no cross-file edges (e.g. a JavaScript project before
// import resolution) collapsed the search to ~0 candidates -- producing
// phantom "Semantic Score: 0.00" rows for every surviving file.
const minSemanticSearchResults = 20

// contextResult is a document that survived both the topological traversal
// and the semantic score boundary.
type contextResult struct {
	Path  string
	Doc   *ir.Document
	Score float32
}

// snippet is a contiguous slice of a source file bounded by an entity's
// start_line/end_line metadata. The whole file is the fallback when a
// document carries no line-annotated entities.
type snippet struct {
	Path      string
	StartLine int
	EndLine   int
	Label     string
	Content   string
}

// noiseSnippetTypes are entity types whose lines duplicate structural
// information already captured by the graph -- import/package are link
// edges, reference mentions resolve to those links -- so rendering them
// as code snippets wastes the token budget on the least informative
// lines of a file (a Kotlin service's import list can exceed half a
// 1500-token context before any declaration appears). They are still
// queryable via get_document_links/get_document_details; get_narrowed_context
// simply stops injecting them as body text.
var noiseSnippetTypes = map[string]bool{
	"import":    true,
	"package":   true,
	"reference": true,
}

// narrowedJSON is the structured payload returned when Format == "json".
type narrowedJSON struct {
	EntryPath   string               `json:"entryPath"`
	SearchQuery string               `json:"searchQuery"`
	SourceType  string               `json:"sourceType,omitempty"`
	BuiltAt     string               `json:"builtAt,omitempty"`
	Truncated   bool                 `json:"truncated,omitempty"`
	Results     []narrowedJSONResult `json:"results"`
}

type narrowedJSONResult struct {
	Path          string                `json:"path"`
	Format        string                `json:"format"`
	SemanticScore float32               `json:"semanticScore,omitempty"`
	Stale         bool                  `json:"stale,omitempty"`
	ReadError     string                `json:"readError,omitempty"`
	Snippets      []narrowedJSONSnippet `json:"snippets"`
}

type narrowedJSONSnippet struct {
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	Label     string `json:"label,omitempty"`
	Content   string `json:"content"`
}

// --- Registration Pipeline ---

func (s *Server) registerTools() error {
	// 1. Tool for inspecting document details and its nested entities
	if err := s.metoroServer.RegisterTool(
		"get_document_details",
		"Retrieves metadata properties and extracted AST entities for an explicit file path.",
		s.handleGetDocumentDetails,
	); err != nil {
		return err
	}

	// 2. Tool for cross-reference link navigation
	if err := s.metoroServer.RegisterTool(
		"get_document_links",
		"Retrieves structural incoming and outgoing semantic links for an analyzed document path.",
		s.handleGetDocumentLinks,
	); err != nil {
		return err
	}

	// 3. Tool for format discovery filtering
	if err := s.metoroServer.RegisterTool(
		"list_documents_by_format",
		"Filters and lists files matching a specific format category.",
		s.handleListDocumentsByFormat,
	); err != nil {
		return err
	}

	// 4. Global Index lookup tool for structural elements
	if err := s.metoroServer.RegisterTool(
		"find_entities_by_type",
		"Scans the global AST index to locate specific structures like structs or interfaces across all files.",
		s.handleFindEntitiesByType,
	); err != nil {
		return err
	}

	// 5. Semantic Vector Search Pruning Tool
	if err := s.metoroServer.RegisterTool(
		"get_narrowed_context",
		"Retrieve a narrowed, highly scoped code context using hybrid topological and semantic vector similarity search to conserve LLM token budget.",
		s.handleGetNarrowedContext,
	); err != nil {
		return err
	}

	// 6. Cluster inspection tools: high-level domain groupings of the graph
	// nodes (by directory tree, Go package, or network coupling).
	if err := s.metoroServer.RegisterTool(
		"list_clusters",
		"Lists the graph node clusters grouped by directory tree, Go module, or network coupling.",
		s.handleListClusters,
	); err != nil {
		return err
	}

	if err := s.metoroServer.RegisterTool(
		"get_cluster",
		"Retrieves the members and metadata of a specific graph node cluster.",
		s.handleGetCluster,
	); err != nil {
		return err
	}

	// 7. Global centrality ("God Node") metrics tool
	if err := s.metoroServer.RegisterTool(
		"get_graph_metrics",
		"Retrieves global centrality metrics (degree, weighted degree, PageRank) and hub ('God Node') documents computed over the graph.",
		s.handleGetGraphMetrics,
	); err != nil {
		return err
	}

	return nil
}

// --- Core Handlers Logic ---

func (s *Server) handleGetNarrowedContext(args NarrowContextArgs) (*mcp_golang.ToolResponse, error) {
	var err error
	var matches []embedx.Result

	if s.graph == nil {
		return nil, fmt.Errorf("graph layer state is uninitialized")
	}
	if s.embedEngine == nil || s.embedder == nil {
		return nil, fmt.Errorf("semantic search engine vector indices are uninitialized")
	}

	if args.MaxHops <= 0 {
		args.MaxHops = 1 // Default to immediate neighbors
	}
	if args.MinScore <= 0.0 {
		args.MinScore = 0.65 // Prune aggressive out-of-bounds noise by default
	}

	ctx := context.Background()

	// Step 1: Generate semantic query vector on the fly using our native pipeline
	queryVector, err := s.embedder.EmbedText(ctx, args.SearchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to vectorise search boundary intent: %w", err)
	}

	// Resolve the entry point. A single indexed file seeds one entry; a
	// directory expands to every indexed document beneath it, so callers can
	// ask for an entire subsystem without guessing a filename first. Anything
	// else is rejected with a clear error instead of silently returning empty.
	entrySet := make(map[string]bool)
	if _, exists := s.graph.Documents[args.EntryPath]; exists {
		entrySet[args.EntryPath] = true
	} else {
		prefix := args.EntryPath
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		for p := range s.graph.Documents {
			if strings.HasPrefix(p, prefix) {
				entrySet[p] = true
			}
		}
		if len(entrySet) == 0 {
			return nil, fmt.Errorf("entry path %q is neither an indexed file nor a directory containing one", args.EntryPath)
		}
	}

	// Track structural topological neighbors. Package nodes expand to every
	// member file, so a single "imports" edge pulls in a whole dependency
	// package in one hop (cross-file package indexing).
	topologicalPaths := make(map[string]bool, len(entrySet))
	currentLevel := make([]string, 0, len(entrySet))
	for entry := range entrySet {
		topologicalPaths[entry] = true
		currentLevel = append(currentLevel, entry)
	}

	// Traverse link graph topology paths
	for hop := 0; hop < args.MaxHops; hop++ {
		var nextLevel []string

		for _, sourcePath := range currentLevel {
			for _, link := range s.graph.Links {
				if link.Weight < 0.5 {
					continue
				}
				// Optional provenance filter: restrict hops to links of
				// one source type (facts vs heuristics). Empty means all.
				if args.SourceType != "" && ir.NormalizeLinkSource(link.SourceType) != args.SourceType {
					continue
				}

				srcFiles := s.nodePaths(link.SourceID)
				tgtFiles := s.nodePaths(link.TargetID)

				if containsPath(srcFiles, sourcePath) {
					for _, tgtFile := range tgtFiles {
						if excludedPath(tgtFile, args.Exclude) || topologicalPaths[tgtFile] {
							continue
						}
						topologicalPaths[tgtFile] = true
						nextLevel = append(nextLevel, tgtFile)
					}
				}
				if containsPath(tgtFiles, sourcePath) {
					for _, srcFile := range srcFiles {
						if excludedPath(srcFile, args.Exclude) || topologicalPaths[srcFile] {
							continue
						}
						topologicalPaths[srcFile] = true
						nextLevel = append(nextLevel, srcFile)
					}
				}
			}
		}
		currentLevel = nextLevel
	}

	// Step 2: Use goembedx.Embedder to perform semantic similarity query matching
	// We request up to len(topologicalPaths)*2 results to cover docs and internal
	// entities, floored at minSemanticSearchResults so a sparse topology frontier
	// (few cross-file edges) never starves the semantic pass to ~0 candidates.
	candidates := len(topologicalPaths) * 2
	if candidates < minSemanticSearchResults {
		candidates = minSemanticSearchResults
	}
	matches, err = s.embedEngine.Search(queryVector, candidates)
	if err != nil {
		return nil, fmt.Errorf("vector database query retrieval failed: %w", err)
	}

	// Map out valid semantic nodes meeting score metrics
	semanticMatches := make(map[string]float32)
	entityScores := make(map[string]float32)
	for _, match := range matches {
		if match.Score >= args.MinScore {
			// Extract clean root file path from Entity ID strings if necessary
			cleanPath := strings.Split(match.ID, "#")[0]

			// Keep the highest similarity score if multiple entities match within the same file
			if existingScore, exists := semanticMatches[cleanPath]; !exists || match.Score > existingScore {
				semanticMatches[cleanPath] = match.Score
			}
			// Keep per-entity scores so the renderers can surface the
			// best-matching declarations first instead of emitting a file
			// in source order (where imports and early boilerplate would
			// otherwise consume the token budget first).
			if existingScore, exists := entityScores[match.ID]; !exists || match.Score > existingScore {
				entityScores[match.ID] = match.Score
			}
		}
	}

	// Step 3: Intersect structural graph neighbors with semantic scores to build
	// a high-relevance manifest. Entry files (or every file under a directory
	// entry) always survive so a caller can never empty out the very code they
	// asked to investigate.
	results := make([]contextResult, 0, len(topologicalPaths))
	for path := range topologicalPaths {
		if excludedPath(path, args.Exclude) {
			continue
		}
		score, meetsSemanticBounds := semanticMatches[path]
		if !meetsSemanticBounds && !entrySet[path] {
			continue
		}

		doc, exists := s.graph.Documents[path]
		if !exists {
			continue
		}
		results = append(results, contextResult{Path: path, Doc: doc, Score: score})
	}

	// Entry files first (ranked by semantic score among themselves), then the
	// rest of the frontier ranked by semantic score.
	sort.Slice(results, func(i, j int) bool {
		ei, ej := entrySet[results[i].Path], entrySet[results[j].Path]
		if ei != ej {
			return ei
		}
		return results[i].Score > results[j].Score
	})

	if strings.EqualFold(args.Format, "json") {
		return s.renderNarrowedJSON(args, results, entityScores)
	}
	return s.renderNarrowedMarkdown(args, results, entityScores)
}

// renderNarrowedMarkdown formats the context manifest as human-readable
// Markdown. Each file is sliced into entity-bounded snippets (falling back
// to the whole file when no line metadata exists) so only the relevant
// blocks are injected into the LLM context. Snippets that matched the
// semantic query surface first; noisy one-line entities (imports,
// packages, reference mentions) are skipped entirely.
func (s *Server) renderNarrowedMarkdown(args NarrowContextArgs, results []contextResult, entityScores map[string]float32) (*mcp_golang.ToolResponse, error) {
	var sb strings.Builder
	sb.WriteString("## Semantically Narrowed Code Context\n")
	sb.WriteString(fmt.Sprintf("- **Entrypoint Target:** `%s`\n", args.EntryPath))
	sb.WriteString(fmt.Sprintf("- **Semantic Intent filter:** \"%s\"\n", args.SearchQuery))
	if args.SourceType != "" {
		sb.WriteString(fmt.Sprintf("- **Provenance filter:** `%s` links only\n", args.SourceType))
	} else {
		sb.WriteString("- **Provenance filter:** all links (extracted + inferred)\n")
	}
	if !s.graph.BuiltAt.IsZero() {
		sb.WriteString(fmt.Sprintf("- **Graph snapshot:** %s\n", s.graph.BuiltAt.UTC().Format("2006-01-02 15:04:05 UTC")))
	} else {
		sb.WriteString("- **Graph snapshot:** unknown (legacy graph, no build timestamp)\n")
	}
	if args.MaxTokens > 0 {
		sb.WriteString(fmt.Sprintf("- **Token budget:** ~%d tokens\n", args.MaxTokens))
	}
	sb.WriteString("\nThe combination of topological link analysis and vector distance filtering isolated these high-relevance blocks:\n\n")

	totalTokens := 0
	overBudget := false
	for _, r := range results {
		if overBudget {
			break
		}

		stale := s.isStale(r.Doc)
		sb.WriteString(fmt.Sprintf("### File: `%s` (Format: %s, Semantic Score: %.2f)\n", r.Path, r.Doc.Format, r.Score))
		sb.WriteString(fmt.Sprintf("- **AST Structures Found:** %d\n", len(r.Doc.Entities)))
		if dm, ok := s.graph.Metrics.Documents[r.Path]; ok && dm.IsHub {
			sb.WriteString("- **Hub (God Node):** YES — central node everything passes through\n")
		}
		switch {
		case stale:
			sb.WriteString("- **Staleness:** STALE (modified after graph snapshot)\n")
		case s.graph.BuiltAt.IsZero():
			sb.WriteString("- **Staleness:** unknown (legacy snapshot)\n")
		default:
			sb.WriteString("- **Staleness:** fresh\n")
		}

		// Attempt to read the file natively from disk and slice it into
		// entity-bounded snippets so we only inject relevant blocks.
		content, err := os.ReadFile(r.Path)
		if err != nil {
			sb.WriteString("```\n[Error reading source code file contents from disk]\n```\n\n")
			continue
		}

		lang := fenceLanguage(r.Doc.Format)
		snips := entitySnippets(r.Path, content, r.Doc.Entities, entityScores)
		if len(snips) == 0 {
			snips = []snippet{{
				Path:      r.Path,
				StartLine: 1,
				EndLine:   strings.Count(string(content), "\n") + 1,
				Label:     "file body",
				Content:   string(content),
			}}
		}
		sb.WriteString(fmt.Sprintf("- **Snippets:** %d\n\n", len(snips)))

		for _, sn := range snips {
			block := fmt.Sprintf("```%s\n// %s (lines %d-%d)\n%s\n```\n\n", lang, sn.Label, sn.StartLine, sn.EndLine, sn.Content)
			tok := estimateTokens(block)
			if args.MaxTokens > 0 && totalTokens+tok > args.MaxTokens {
				if room := args.MaxTokens - totalTokens; room > 0 {
					sb.WriteString(truncateChars(block, room*4))
					sb.WriteString("\n*[context truncated to honor token budget]*\n\n")
				}
				overBudget = true
				break
			}
			sb.WriteString(block)
			totalTokens += tok
		}
	}

	if len(results) == 0 {
		sb.WriteString("*No overlapping code structures discovered matching both topological distance and the minimum semantic query boundary score.*")
	} else if overBudget {
		sb.WriteString("*[token budget reached; remaining results omitted]*\n")
	}

	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(sb.String())), nil
}

// renderNarrowedJSON formats the same manifest as a structured JSON payload,
// useful for programmatic consumers that parse the tool output.
func (s *Server) renderNarrowedJSON(args NarrowContextArgs, results []contextResult, entityScores map[string]float32) (*mcp_golang.ToolResponse, error) {
	out := narrowedJSON{
		EntryPath:   args.EntryPath,
		SearchQuery: args.SearchQuery,
		SourceType:  args.SourceType,
	}
	if !s.graph.BuiltAt.IsZero() {
		out.BuiltAt = s.graph.BuiltAt.UTC().Format(time.RFC3339)
	}

	totalTokens := 0
	for _, r := range results {
		jr := narrowedJSONResult{
			Path:          r.Path,
			Format:        r.Doc.Format,
			SemanticScore: r.Score,
			Stale:         s.isStale(r.Doc),
			Snippets:      []narrowedJSONSnippet{},
		}
		content, err := os.ReadFile(r.Path)
		if err != nil {
			jr.ReadError = "file not found on disk"
		} else {
			snips := entitySnippets(r.Path, content, r.Doc.Entities, entityScores)
			if len(snips) == 0 {
				snips = []snippet{{
					Path:      r.Path,
					StartLine: 1,
					EndLine:   strings.Count(string(content), "\n") + 1,
					Label:     "file body",
					Content:   string(content),
				}}
			}
			for _, sn := range snips {
				jr.Snippets = append(jr.Snippets, narrowedJSONSnippet{
					StartLine: sn.StartLine,
					EndLine:   sn.EndLine,
					Label:     sn.Label,
					Content:   sn.Content,
				})
				totalTokens += estimateTokens(sn.Content)
			}
		}
		out.Results = append(out.Results, jr)

		if args.MaxTokens > 0 && totalTokens >= args.MaxTokens {
			out.Truncated = true
			break
		}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal narrowed context JSON: %w", err)
	}
	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(string(data))), nil
}

// nodePaths expands a graph node ID into the set of document paths it
// represents. Document and entity IDs map to their owning file; package
// nodes expand to every indexed member file.
func (s *Server) nodePaths(id string) []string {
	if strings.HasPrefix(id, ir.PackageNodePrefix) {
		dir := strings.TrimPrefix(id, ir.PackageNodePrefix)
		if pkg, ok := s.graph.Packages[dir]; ok {
			return pkg.Files
		}
		return nil
	}
	return []string{strings.SplitN(id, "#", 2)[0]}
}

// containsPath reports whether path is present in files.
func containsPath(files []string, path string) bool {
	for _, f := range files {
		if f == path {
			return true
		}
	}
	return false
}

// excludedPath reports whether a file path matches any exclusion pattern.
// Patterns may be path prefixes (e.g. "vendor") or globs (e.g. "*.generated.go").
func excludedPath(path string, patterns []string) bool {
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
		if ok, _ := filepath.Match(p, path); ok {
			return true
		}
		if ok, _ := filepath.Match(p, filepath.Base(path)); ok {
			return true
		}
	}
	return false
}

// isStale reports whether a document was modified after the graph snapshot
// was built. Graphs without a build timestamp are never reported stale.
func (s *Server) isStale(doc *ir.Document) bool {
	return !s.graph.BuiltAt.IsZero() && doc.UpdatedAt.After(s.graph.BuiltAt)
}

// entitySnippets slices file content into one snippet per line-annotated
// entity, collapsing duplicate ranges. Noisy entity types (imports,
// packages, reference mentions) are skipped, and remaining snippets are
// ordered by their semantic match score (descending, stable so equal
// scores keep source order) so the most relevant declarations surface
// before the token budget runs out. Documents without line metadata
// yield no snippets (callers fall back to the whole file).
func entitySnippets(path string, content []byte, entities []ir.Entity, entityScores map[string]float32) []snippet {
	lines := strings.Split(string(content), "\n")
	type ranked struct {
		entity ir.Entity
		start  int
		end    int
	}
	var eligible []ranked
	seen := make(map[[2]int]bool)
	for _, e := range entities {
		if noiseSnippetTypes[e.Type] {
			continue
		}
		start, end := entityLineRange(e)
		if start <= 0 {
			continue
		}
		if start > len(lines) {
			start = len(lines)
		}
		if end > len(lines) {
			end = len(lines)
		}
		key := [2]int{start, end}
		if seen[key] {
			continue
		}
		seen[key] = true
		eligible = append(eligible, ranked{entity: e, start: start, end: end})
	}

	sort.SliceStable(eligible, func(i, j int) bool {
		return entityScores[eligible[i].entity.ID] > entityScores[eligible[j].entity.ID]
	})

	snips := make([]snippet, 0, len(eligible))
	for _, r := range eligible {
		snips = append(snips, snippet{
			Path:      path,
			StartLine: r.start,
			EndLine:   r.end,
			Label:     r.entity.Type + " " + r.entity.Name,
			Content:   strings.Join(lines[r.start-1:r.end], "\n"),
		})
	}
	return snips
}

// entityLineRange parses the start_line/end_line metadata keys every
// tree-sitter extractor sets on its entities.
func entityLineRange(e ir.Entity) (int, int) {
	start, err := strconv.Atoi(e.Metadata["start_line"])
	if err != nil || start <= 0 {
		return 0, 0
	}
	end, err := strconv.Atoi(e.Metadata["end_line"])
	if err != nil || end < start {
		end = start
	}
	return start, end
}

// fenceLanguage maps an IR format key onto a markdown code fence hint.
func fenceLanguage(format string) string {
	switch format {
	case "golang":
		return "go"
	case "javascript":
		return "js"
	case "typescript":
		return "ts"
	case "tsx":
		return "tsx"
	default:
		return format
	}
}

// estimateTokens is a rough tokens-per-character heuristic used to honor a
// caller-supplied token budget without a full tokenizer.
func estimateTokens(text string) int {
	return len([]rune(text)) / 4
}

// truncateChars cuts text to a maximum rune count, appending a marker so
// callers can see the cut happened.
func truncateChars(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func (s *Server) handleGetDocumentDetails(args DocumentQueryArgs) (*mcp_golang.ToolResponse, error) {
	if s.graph == nil {
		return nil, fmt.Errorf("graph layer state is uninitialized")
	}

	doc, exists := s.graph.Documents[args.Path]
	if !exists {
		return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(fmt.Sprintf("Error: Document path '%s' not found in the current index.", args.Path))), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Document Details: %s\n", args.Path))
	sb.WriteString(fmt.Sprintf("* **Format:** %s\n", doc.Format))
	sb.WriteString(fmt.Sprintf("* **Size:** %d bytes\n", doc.Size))
	sb.WriteString(fmt.Sprintf("* **Last Updated:** %s\n", doc.UpdatedAt.Format("2006-01-02 15:04:05")))

	if len(doc.Metadata) > 0 {
		sb.WriteString("\n**Document Metadata:**\n")
		for k, v := range doc.Metadata {
			sb.WriteString(fmt.Sprintf("- **%s:** %s\n", k, v))
		}
	}

	// Report the document's global centrality ("God Node") scores when the
	// graph snapshot carries them. Hub documents are the architectural
	// touchpoints (central DB drivers, middleware, routers) worth reading
	// first when exploring a subsystem.
	if dm, ok := s.graph.Metrics.Documents[doc.Path]; ok {
		sb.WriteString("\n**Centrality Metrics (God Node):**\n")
		if dm.IsHub {
			sb.WriteString("- **Hub:** YES\n")
		} else {
			sb.WriteString("- **Hub:** no\n")
		}
		sb.WriteString(fmt.Sprintf("- **Degree:** %d\n", dm.Degree))
		sb.WriteString(fmt.Sprintf("- **Weighted Degree:** %.2f\n", dm.WeightedDegree))
		sb.WriteString(fmt.Sprintf("- **PageRank:** %.5f\n", dm.PageRank))
	}

	// Loop through and format the deeply nested structural entities
	if len(doc.Entities) > 0 {
		sb.WriteString("\n**Extracted Semantic Entities (AST):**\n")
		for _, entity := range doc.Entities {
			sb.WriteString(fmt.Sprintf("- **%s** (`%s`) - Name: %s\n", entity.ID, entity.Type, entity.Name))
			for mk, mv := range entity.Metadata {
				sb.WriteString(fmt.Sprintf("  - *%s:* %s\n", mk, mv))
			}
		}
	} else {
		sb.WriteString("\n*No structural entities extracted from this document format.*\n")
	}

	// List the clusters this document belongs to so callers can see the
	// higher-level subsystems it participates in.
	var memberClusters []string
	for _, c := range s.graph.Clusters {
		for _, m := range c.Members {
			if m == doc.Path {
				memberClusters = append(memberClusters, c.ID)
				break
			}
		}
	}
	if len(memberClusters) > 0 {
		sb.WriteString("\n**Member of Clusters:**\n")
		for _, id := range memberClusters {
			sb.WriteString(fmt.Sprintf("- `%s`\n", id))
		}
	}

	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(sb.String())), nil
}

func (s *Server) handleGetDocumentLinks(args DocumentQueryArgs) (*mcp_golang.ToolResponse, error) {
	if s.graph == nil {
		return nil, fmt.Errorf("graph layer state is uninitialized")
	}

	// Clean target path parameter to prevent trailing space bugs
	targetPath := strings.TrimSpace(args.Path)

	if _, exists := s.graph.Documents[targetPath]; !exists {
		return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(fmt.Sprintf("Error: Source document '%s' does not exist in the index.", targetPath))), nil
	}

	var outgoingLinks []string
	var incomingLinks []string

	// Define an explicit structural anchor separator (e.g., "path/file.go#Entity")
	pathAnchor := targetPath + "#"

	for _, link := range s.graph.Links {
		// Outgoing check: Matches exact path OR any entity internal prefix anchor
		isOutgoing := link.SourceID == targetPath || strings.HasPrefix(link.SourceID, pathAnchor)

		// Incoming check: Matches exact path OR any entity internal prefix anchor
		isIncoming := link.TargetID == targetPath || strings.HasPrefix(link.TargetID, pathAnchor)

		if isOutgoing {
			outgoingLinks = append(outgoingLinks, fmt.Sprintf("- **To:** %s (Type: %s, Source: %s, Weight: %.2f)", link.TargetID, link.Type, ir.NormalizeLinkSource(link.SourceType), link.Weight))
		}
		if isIncoming {
			incomingLinks = append(incomingLinks, fmt.Sprintf("- **From:** %s (Type: %s, Source: %s, Weight: %.2f)", link.SourceID, link.Type, ir.NormalizeLinkSource(link.SourceType), link.Weight))
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Semantic Cross-References for: `%s`\n\n", targetPath))

	sb.WriteString("#### Outgoing Links (Dependencies / Calls / Structural):\n")
	if len(outgoingLinks) == 0 {
		sb.WriteString("*No outgoing references discovered.*\n")
	} else {
		sb.WriteString(strings.Join(outgoingLinks, "\n") + "\n")
	}

	sb.WriteString("\n#### Incoming Links (Dependents / Referenced By):\n")
	if len(incomingLinks) == 0 {
		sb.WriteString("*No incoming references discovered.*\n")
	} else {
		sb.WriteString(strings.Join(incomingLinks, "\n") + "\n")
	}

	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(sb.String())), nil
}

func (s *Server) handleListDocumentsByFormat(args FilterQueryArgs) (*mcp_golang.ToolResponse, error) {
	if s.graph == nil {
		return nil, fmt.Errorf("graph layer state is uninitialized")
	}

	var matchedDocs []string
	targetFormat := strings.ToLower(args.Format)

	for path, doc := range s.graph.Documents {
		if strings.ToLower(doc.Format) == targetFormat {
			matchedDocs = append(matchedDocs, fmt.Sprintf("* `%s` (%d bytes, %d entities)", path, doc.Size, len(doc.Entities)))
		}
	}

	if len(matchedDocs) == 0 {
		return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(fmt.Sprintf("No registered documents match format type: '%s'.", args.Format))), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Indexed Documents Matching Format: '%s'\n\n", args.Format))
	sb.WriteString(strings.Join(matchedDocs, "\n"))

	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(sb.String())), nil
}

func (s *Server) handleFindEntitiesByType(args EntityFilterArgs) (*mcp_golang.ToolResponse, error) {
	if s.graph == nil {
		return nil, fmt.Errorf("graph layer state is uninitialized")
	}

	var matchedEntities []string
	targetType := strings.ToLower(args.Type)

	// Scan through documents to search across the global domain entity index mappings
	for path, doc := range s.graph.Documents {
		for _, entity := range doc.Entities {
			if strings.ToLower(entity.Type) == targetType {
				matchedEntities = append(matchedEntities, fmt.Sprintf(
					"* **%s** (`%s`) in `%s` (Line/Context info if present: %v)",
					entity.Name, entity.ID, path, entity.Metadata,
				))
			}
		}
	}

	if len(matchedEntities) == 0 {
		return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(fmt.Sprintf("No extracted elements found matching structural entity type: '%s'.", args.Type))), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Global AST Index Search: '%s' Types Discovered\n\n", args.Type))
	sb.WriteString(strings.Join(matchedEntities, "\n"))

	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(sb.String())), nil
}

func (s *Server) handleListClusters(args ClusterListArgs) (*mcp_golang.ToolResponse, error) {
	if s.graph == nil {
		return nil, fmt.Errorf("graph layer state is uninitialized")
	}

	var sb strings.Builder
	sb.WriteString("### Graph Node Clusters\n")
	emitted := 0
	// Fixed order so the report reads directory -> module -> network.
	for _, kind := range []string{ir.ClusterKindDirectory, ir.ClusterKindModule, ir.ClusterKindNetwork} {
		var lines []string
		for _, c := range s.graph.Clusters {
			if args.Kind != "" && !strings.EqualFold(c.Kind, args.Kind) {
				continue
			}
			if c.Kind != kind {
				continue
			}
			lines = append(lines, fmt.Sprintf("* **%s** (%s) — %d files — members: %s", c.ID, c.Kind, c.Size, previewMembers(c.Members, 3)))
		}
		if len(lines) == 0 {
			continue
		}
		emitted += len(lines)
		sb.WriteString(fmt.Sprintf("\n#### %s\n", kind))
		sb.WriteString(strings.Join(lines, "\n") + "\n")
	}

	if emitted == 0 {
		sb.WriteString("*No clusters found")
		if args.Kind != "" {
			sb.WriteString(fmt.Sprintf(" matching kind '%s'", args.Kind))
		}
		sb.WriteString(".*\n")
	}

	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(sb.String())), nil
}

func (s *Server) handleGetCluster(args ClusterDetailArgs) (*mcp_golang.ToolResponse, error) {
	if s.graph == nil {
		return nil, fmt.Errorf("graph layer state is uninitialized")
	}

	for _, c := range s.graph.Clusters {
		if c.ID == args.ID {
			return s.renderCluster(c), nil
		}
	}

	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(fmt.Sprintf("Error: Cluster '%s' not found in the current graph.", args.ID))), nil
}

// renderCluster formats a cluster and its member documents as markdown.
func (s *Server) renderCluster(c ir.Cluster) *mcp_golang.ToolResponse {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Cluster: `%s`\n", c.ID))
	sb.WriteString(fmt.Sprintf("* **Kind:** %s\n", c.Kind))
	sb.WriteString(fmt.Sprintf("* **Name:** %s\n", c.Name))
	sb.WriteString(fmt.Sprintf("* **Size:** %d files\n", c.Size))

	sb.WriteString("\n**Members:**\n")
	if len(c.Members) == 0 {
		sb.WriteString("*No members.*\n")
	} else {
		for _, m := range c.Members {
			format := "unknown"
			if doc, ok := s.graph.Documents[m]; ok && doc.Format != "" {
				format = doc.Format
			}
			sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", m, format))
		}
	}

	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(sb.String()))
}

// previewMembers joins the first n member paths into a compact inline
// list, appending "…" when more members remain.
func previewMembers(members []string, n int) string {
	if len(members) == 0 {
		return "none"
	}
	if n <= 0 {
		n = len(members)
	}
	if len(members) <= n {
		return "`" + strings.Join(members, "`, `") + "`"
	}
	return "`" + strings.Join(members[:n], "`, `") + "`, …"
}

// handleGetGraphMetrics reports the global centrality ("God Node") scores
// computed over the document-level graph, ranked so hubs come first and
// ties break on PageRank (then path). MaxResults caps the report; 0 means
// every scored document.
func (s *Server) handleGetGraphMetrics(args MetricsArgs) (*mcp_golang.ToolResponse, error) {
	if s.graph == nil {
		return nil, fmt.Errorf("graph layer state is uninitialized")
	}

	if s.graph.Metrics.Documents == nil {
		return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(
			"Graph carries no centrality metrics. Rebuild the graph with a recent kg build to populate hub/'God Node' scores.")), nil
	}

	type ranked struct {
		path string
		dm   ir.DocumentMetrics
	}
	docs := make([]ranked, 0, len(s.graph.Metrics.Documents))
	for path, dm := range s.graph.Metrics.Documents {
		docs = append(docs, ranked{path: path, dm: dm})
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].dm.IsHub != docs[j].dm.IsHub {
			return docs[i].dm.IsHub
		}
		if docs[i].dm.PageRank != docs[j].dm.PageRank {
			return docs[i].dm.PageRank > docs[j].dm.PageRank
		}
		return docs[i].path < docs[j].path
	})

	if args.MaxResults > 0 && len(docs) > args.MaxResults {
		docs = docs[:args.MaxResults]
	}

	var sb strings.Builder
	sb.WriteString("### Graph Centrality Metrics (God Node detection)\n")
	sb.WriteString(fmt.Sprintf("- **Documents indexed:** %d\n", len(s.graph.Documents)))
	sb.WriteString(fmt.Sprintf("- **Hub count:** %d\n", s.graph.Metrics.HubCount))
	if s.graph.BuiltAt.IsZero() {
		sb.WriteString("- **Graph snapshot:** unknown (legacy graph, no build timestamp)\n")
	} else {
		sb.WriteString(fmt.Sprintf("- **Graph snapshot:** %s\n", s.graph.BuiltAt.UTC().Format("2006-01-02 15:04:05 UTC")))
	}

	sb.WriteString("\nRanked by PageRank; hubs (HUB) first:\n\n")
	if len(docs) == 0 {
		sb.WriteString("*No documents scored.*\n")
	} else {
		for _, d := range docs {
			marker := "    "
			if d.dm.IsHub {
				marker = "[HUB]"
			}
			sb.WriteString(fmt.Sprintf("* %s `%s` — degree %d, weighted %.2f, PageRank %.5f\n",
				marker, d.path, d.dm.Degree, d.dm.WeightedDegree, d.dm.PageRank))
		}
	}

	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(sb.String())), nil
}
