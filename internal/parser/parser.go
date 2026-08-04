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
		entities, links, err := extractGoData(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("golang parser failed: %w", err)
		}
		doc.Entities = entities
		doc.Links = links

	case "pdf":
		entities, err := extractPDFData(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("pdf parser failed: %w", err)
		}
		doc.Entities = entities
		doc.Metadata["processor"] = "pdf-extractor"

	case "spreadsheet":
		entities, err := extractSpreadsheetData(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("spreadsheet parser failed: %w", err)
		}
		doc.Entities = entities
		doc.Metadata["processor"] = "spreadsheet-extractor"

	case "markdown":
		entities, err := extractMarkdownData(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("markdown parser failed: %w", err)
		}
		doc.Entities = entities
		doc.Metadata["processor"] = "markdown-extractor"

	case "json":
		entities, err := extractJSONData(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("json parser failed: %w", err)
		}
		doc.Entities = entities
		doc.Metadata["processor"] = "json-extractor"

	case "yaml":
		entities, err := extractYAMLData(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("yaml parser failed: %w", err)
		}
		doc.Entities = entities
		doc.Metadata["processor"] = "yaml-extractor"

	case "toml":
		entities, err := extractTOMLData(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("toml parser failed: %w", err)
		}
		doc.Entities = entities
		doc.Metadata["processor"] = "toml-extractor"

	case "javascript":
		entities, err := extractJSData(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("javascript parser failed: %w", err)
		}
		doc.Entities = entities
		doc.Metadata["processor"] = "js-extractor"

	case "typescript", "tsx":
		extract := extractTSData
		if file.Language == "tsx" {
			extract = extractTSXData
		}
		entities, err := extract(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("typescript parser failed: %w", err)
		}
		doc.Entities = entities
		doc.Metadata["processor"] = "ts-extractor"

	case "dockerfile":
		entities, err := extractDockerfileData(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("dockerfile parser failed: %w", err)
		}
		doc.Entities = entities
		doc.Metadata["processor"] = "dockerfile-extractor"

	case "make":
		entities, err := extractMakefileData(file.Path)
		if err != nil {
			return ir.Document{}, fmt.Errorf("make parser failed: %w", err)
		}
		doc.Entities = entities
		doc.Metadata["processor"] = "make-extractor"

	default:
		// Generic fallback for plain text or unknown formats --
		// still produces a Document so unstructured files appear
		// in the graph even if we can't extract entities from them yet.
		doc.Metadata["processor"] = "generic-unstructured-extractor"
	}

	return doc, nil
}
