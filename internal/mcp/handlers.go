package mcp

import (
	"context"
	"fmt"
	"github.com/ldaidone/goembedx/pkg/embedx"
	"github.com/metoro-io/mcp-golang"
	"os"
	"strings"
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

type NarrowContextArgs struct {
	EntryPath   string  `json:"entryPath" jsonschema:"required,description=The file path where the bug or feature investigation starts."`
	SearchQuery string  `json:"searchQuery" jsonschema:"required,description=The semantic intent or feature description to slice context against (e.g. error handling in DB routines)."`
	MaxHops     int     `json:"maxHops" jsonschema:"description=Degrees of structural topology separation to traverse. Defaults to 1."`
	MinScore    float32 `json:"minScore" jsonschema:"description=Minimum semantic similarity score bounds between 0.0 and 1.0. Defaults to 0.65."`
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

	// Track structural topological neighbors
	topologicalPaths := make(map[string]bool)
	topologicalPaths[args.EntryPath] = true

	currentLevel := []string{args.EntryPath}

	// Traverse link graph topology paths
	for hop := 0; hop < args.MaxHops; hop++ {
		var nextLevel []string

		for _, sourcePath := range currentLevel {
			pathAnchor := sourcePath + "#"

			for _, link := range s.graph.Links {
				if link.Weight < 0.5 {
					continue
				}

				srcFile := strings.Split(link.SourceID, "#")[0]
				tgtFile := strings.Split(link.TargetID, "#")[0]

				if link.SourceID == sourcePath || strings.HasPrefix(link.SourceID, pathAnchor) {
					if !topologicalPaths[tgtFile] {
						topologicalPaths[tgtFile] = true
						nextLevel = append(nextLevel, tgtFile)
					}
				}
				if link.TargetID == sourcePath || strings.HasPrefix(link.TargetID, pathAnchor) {
					if !topologicalPaths[srcFile] {
						topologicalPaths[srcFile] = true
						nextLevel = append(nextLevel, srcFile)
					}
				}
			}
		}
		currentLevel = nextLevel
	}

	// Step 2: Use goembedx.Embedder to perform semantic similarity query matching
	// We request up to len(topologicalPaths)*2 results to cover docs and internal entities
	matches, err = s.embedEngine.Search(queryVector, len(topologicalPaths)*2)
	if err != nil {
		return nil, fmt.Errorf("vector database query retrieval failed: %w", err)
	}

	// Map out valid semantic nodes meeting score metrics
	semanticMatches := make(map[string]float32)
	for _, match := range matches {
		if match.Score >= args.MinScore {
			// Extract clean root file path from Entity ID strings if necessary
			cleanPath := strings.Split(match.ID, "#")[0]

			// Keep the highest similarity score if multiple entities match within the same file
			if existingScore, exists := semanticMatches[cleanPath]; !exists || match.Score > existingScore {
				semanticMatches[cleanPath] = match.Score
			}
		}
	}

	// Step 3: Intersect structural graph neighbors with semantic scores to build high-relevance manifest
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Semantically Narrowed Code Context\n"))
	sb.WriteString(fmt.Sprintf("- **Entrypoint Target:** `%s`\n", args.EntryPath))
	sb.WriteString(fmt.Sprintf("- **Semantic Intent filter:** \"%s\"\n\n", args.SearchQuery))
	sb.WriteString("The combination of topological link analysis and vector distance filtering isolated these high-relevance blocks:\n\n")

	hasResults := false
	for path := range topologicalPaths {
		score, meetsSemanticBounds := semanticMatches[path]
		if !meetsSemanticBounds && path != args.EntryPath {
			// Prune node entirely from token injection if it isn't the root file or lacks semantic alignment
			continue
		}

		doc, exists := s.graph.Documents[path]
		if !exists {
			continue
		}

		hasResults = true
		sb.WriteString(fmt.Sprintf("### File: `%s` (Format: %s, Semantic Score: %.2f)\n", path, doc.Format, score))
		sb.WriteString(fmt.Sprintf("- **AST Structures Found:** %d\n", len(doc.Entities)))

		// Attempt to read the snippet natively from disk to pass raw tokens
		if content, err := os.ReadFile(path); err == nil {
			sb.WriteString("```go\n" + string(content) + "\n```\n\n")
		} else {
			sb.WriteString("```\n[Error reading source code file contents from disk]\n```\n\n")
		}
	}

	if !hasResults {
		sb.WriteString("*No overlapping code structures discovered matching both topological distance and the minimum semantic query boundary score.*")
	}

	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(sb.String())), nil
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
			outgoingLinks = append(outgoingLinks, fmt.Sprintf("- **To:** %s (Type: %s, Weight: %.2f)", link.TargetID, link.Type, link.Weight))
		}
		if isIncoming {
			incomingLinks = append(incomingLinks, fmt.Sprintf("- **From:** %s (Type: %s, Weight: %.2f)", link.SourceID, link.Type, link.Weight))
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
