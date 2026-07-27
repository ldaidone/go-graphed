// Package ir defines the intermediate representation shared between
// every pipeline stage (scanner, parser, analyzer, exporter).
// A single, stable IR lets each stage be developed and tested
// independently while still agreeing on the data contract.
package ir

import "time"

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
	Metadata map[string]string
}
