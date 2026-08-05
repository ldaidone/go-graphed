// Package ir defines the intermediate representation shared between
// every pipeline stage (scanner, parser, analyzer, exporter).
// A single, stable IR lets each stage be developed and tested
// independently while still agreeing on the data contract.
package ir

import "time"

// Link provenance tags distinguish edges that were parsed directly from
// source (hard facts) from edges that were derived by heuristics. The tags
// let downstream consumers (MCP handlers, LLM context selectors) separate
// raw facts from guesses.
const (
	// LinkSourceExtracted marks a link parsed directly from the AST,
	// e.g. a "calls" edge between two functions or an "exports" edge.
	LinkSourceExtracted = "extracted"
	// LinkSourceInferred marks a link derived by a heuristic rule,
	// e.g. "implements" from a naming convention, not by parsing.
	LinkSourceInferred = "inferred"
)

// NormalizeLinkSource returns a canonical provenance tag, defaulting
// empty values to LinkSourceExtracted so graphs produced before the
// SourceType field existed remain fully annotated.
func NormalizeLinkSource(s string) string {
	if s == "" {
		return LinkSourceExtracted
	}
	return s
}

// NormalizeLinks applies NormalizeLinkSource to every link so no
// extractor or hand-built fixture can emit an untagged edge.
func NormalizeLinks(links []Link) []Link {
	for i := range links {
		links[i].SourceType = NormalizeLinkSource(links[i].SourceType)
	}
	return links
}

// PackageNodePrefix marks graph nodes that represent a package directory
// rather than a document (e.g. "package:internal/ir").
const PackageNodePrefix = "package:"

// PackageNodeID returns the graph node identifier for a package directory.
func PackageNodeID(dir string) string {
	return PackageNodePrefix + dir
}

// Cluster kinds describe how a grouping of graph nodes was derived.
// Downstream consumers (MCP handlers, LLM context selectors) use the tag
// to pick the grouping that answers the question being asked: an agent
// hunting for "where does the billing domain live" wants the directory or
// module clusters, while "which files couple tightly together" wants the
// network clusters.
const (
	// ClusterKindDirectory groups documents by their local directory tree.
	ClusterKindDirectory = "directory"
	// ClusterKindModule groups documents by their Go package (module) unit.
	ClusterKindModule = "module"
	// ClusterKindNetwork groups documents by structural coupling derived
	// from a label-propagation pass over the link graph.
	ClusterKindNetwork = "network"
)

// ClusterID returns the graph node identifier for a cluster of the given
// kind keyed by name (e.g. ClusterID("directory", "internal/ir") yields
// "directory:internal/ir"). The kind prefix keeps IDs from different
// grouping strategies distinct.
func ClusterID(kind, name string) string {
	return kind + ":" + name
}

// MetadataHubFlag is the metadata key that flags a document as a global hub
// ("God Node") in the exported graph. Consumers of graph.json can test
// doc.Metadata["is_hub"] == "true" to prioritize core infrastructure over
// helper utilities without parsing the structured metrics table.
const MetadataHubFlag = "is_hub"

// Graph is the final output of the pipeline. It holds every document
// discovered during scanning together with the cross-reference links
// the analyzer inferred between them.
type Graph struct {
	// Documents maps each file path to its enriched metadata.
	// Using a map (instead of a slice) gives O(1) look-ups when
	// the analyzer needs to resolve source/target IDs.
	Documents map[string]*Document
	// Links are the semantic relationships discovered by the analyzer.
	Links []Link
	// Packages aggregates the files that declare the same package in a
	// single directory. Cross-file package indexing exposes "part_of" and
	// "imports" links so traversal can hop between related files without
	// an explicit per-symbol edge.
	Packages map[string]*Package
	// Clusters groups the documents into higher-level domain units -- by
	// directory tree, Go package, or network coupling -- so downstream
	// consumers can present subsystems instead of hundreds of raw nodes.
	Clusters []Cluster
	// Metrics holds the global centrality ("God Node") scores computed over
	// the document-level coupling graph. Documents with IsHub=true are the
	// high-impact architectural touchpoints (central DB drivers, middleware,
	// routers) that everything passes through; LLM context selectors use the
	// flag to prioritize core infrastructure over helper utilities.
	Metrics Metrics
	// BuiltAt records when the graph was assembled. Downstream consumers
	// use it to detect stale documents (files modified after the snapshot).
	BuiltAt time.Time
}

// Package aggregates every indexed file that declares the same package.
// Path is the directory holding the package; Files lists the member
// document paths; Imports lists the package directories this package
// depends on that the analyzer could resolve inside the index.
type Package struct {
	Name       string   // e.g. "ir"
	Path       string   // directory, e.g. "internal/ir"
	ImportPath string   // fully-qualified import path when resolvable
	Files      []string // document paths declaring this package
	Imports    []string // resolved dependency package directories
}

// Cluster groups graph nodes into a higher-level domain unit. Members are
// document paths -- entities group through their owning document and
// package nodes through their member files -- so every member ID resolves
// directly against the Graph.Documents index.
type Cluster struct {
	// ID is a stable, unique identifier such as "directory:internal/ir".
	ID string
	// Name is a readable label such as "internal/ir" or "auth_subsystem".
	Name string
	// Kind is one of ClusterKindDirectory, ClusterKindModule or
	// ClusterKindNetwork, describing how the group was derived.
	Kind string
	// Members lists the document paths that belong to this cluster.
	Members []string
	// Size is the number of member documents, so consumers can rank
	// clusters without counting Members.
	Size int
}

// Metrics aggregates the global centrality ("God Node") scores computed over
// the document-level coupling graph. It is a side table (like Clusters) so
// topological traversal stays unchanged while consumers get a per-document
// view of architectural importance.
type Metrics struct {
	// Documents maps each indexed document path to its centrality scores.
	// Documents that carry no incident links keep zero-valued scores rather
	// than being omitted, so consumers can look up any path directly.
	Documents map[string]DocumentMetrics
	// HubCount is the number of documents flagged as hubs. Stored so
	// consumers can size the hub set without counting IsHub entries.
	HubCount int
}

// DocumentMetrics is the per-document centrality record produced by the
// "God Node" pass. The scores are computed on the same document-level
// coupling graph used for network clustering: entity anchors collapse onto
// their owning file and package nodes expand onto every member file.
type DocumentMetrics struct {
	// Degree is the number of distinct documents this document couples
	// with, after dropping near-noise links below the traversal threshold.
	Degree int
	// WeightedDegree is the sum of incident link weights on the same
	// graph, so a single heavy "part_of" edge counts for more than many
	// weak keyword matches.
	WeightedDegree float64
	// PageRank is the global centrality score from the power-method pass
	// over the weighted graph. It sums to ~1 across the document graph and
	// is proportional to weighted degree on the undirected projection,
	// making it a normalized measure of "everything funnels through here".
	PageRank float64
	// IsHub reports whether the document is one of the graph's most central
	// nodes -- the "God Nodes" (central DB drivers, middleware handlers,
	// main router instances) that everything passes through.
	IsHub bool
}

// Document represents any file the scanner found, annotated with
// the entities a parser extracted from it.  Documents are the
// "nodes" of the knowledge graph; Links connect them.
type Document struct {
	Path      string // e.g., "docs/architecture.pdf", "src/main.go"
	Format    string // language key returned by the scanner (golang, pdf, markdown …)
	Size      int64
	UpdatedAt time.Time
	// Metadata stores free-form key/value pairs such as content hashes,
	// author names, or processor labels.  Because the IR is serialised
	// to JSON, an open-ended map avoids coupling to a fixed struct.
	Metadata map[string]string

	// Entities are the semantic concepts/nodes discovered *inside* this file.
	Entities []Entity

	// Links are the relationships a parser discovered *inside* this file
	// (e.g. a within-file Go call graph).  The analyzer lifts them onto the
	// final graph alongside its own cross-document heuristics.
	Links []Link
}

// Entity is a semantic unit extracted from a Document -- a struct,
// interface, heading, table, or any other notion worth tracking.
// Entities live inside a Document but are also registered in a
// global index so the analyzer can link across files.
type Entity struct {
	ID       string            // Unique identifier within the global context
	Type     string            // e.g., "struct", "topic", "person", "data-table", "image-asset"
	Name     string            // Readable label
	Metadata map[string]string // Contextual attributes (e.g., line number, paragraph, cell range)
}

// Link connects two nodes in the graph.  SourceID and TargetID may
// reference either an Entity ID or a Document path, depending on
// the relationship kind.  Weight carries a confidence score so
// downstream consumers can rank or filter weaker edges.
type Link struct {
	SourceID string  // Can point to an Entity ID or a Document Path
	TargetID string  // Can point to an Entity ID or a Document Path
	Type     string  // e.g., "defines", "references", "contains", "mentions", "extends"
	Weight   float64 // Confidence score or relationship strength (useful for NLP/embeddings)
	// SourceType tags the edge's provenance so downstream consumers can
	// separate hard facts from heuristics. One of LinkSourceExtracted or
	// LinkSourceInferred; empty values normalize to "extracted".
	SourceType string
	Metadata   map[string]string
}
