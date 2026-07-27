// Package parser turns raw scanner output (File) into enriched
// IR Documents.  Each language gets its own extractor function;
// Parse() is the single entry point that dispatches based on the
// language key the scanner assigned.
package parser

import (
	"fmt"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

// Parse processes a single scanned file and extracts its metadata
// and semantic entities.  Keeping this as a free function (rather
// than a method) mirrors the scanner's Stateless design and makes
// concurrent parsing straightforward.
func Parse(file scanner.File) (ir.Document, error) {
	doc := ir.Document{
		Path:     file.Path,
		Format:   file.Language,
		Size:     file.Size,
		Metadata: make(map[string]string),
		Entities: []ir.Entity{},
	}

	// Dispatch to the language-specific extractor.  The switch is
	// intentionally kept shallow -- each case owns exactly one
	// extraction strategy so new languages only need a new case
	// and a corresponding extract* function.
	switch file.Language {
	case "golang":
		entities, err := extractGoData(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("golang parser failed: %w", err)
		}
		doc.Entities = entities

	case "pdf":
		// TODO: Implement PDF extractor (e.g., using a text-extraction library)
		doc.Metadata["processor"] = "pdf-fallback-extractor"

	case "spreadsheet":
		// TODO: Implement Spreadsheet extractor (e.g., using excelize)
		doc.Metadata["processor"] = "excel-fallback-extractor"

	case "markdown":
		// TODO: Implement Markdown heading/reference extractor
		doc.Metadata["processor"] = "markdown-fallback-extractor"

	default:
		// Generic fallback for plain text or unknown formats --
		// still produces a Document so unstructured files appear
		// in the graph even if we can't extract entities from them yet.
		doc.Metadata["processor"] = "generic-unstructured-extractor"
	}

	return doc, nil
}
