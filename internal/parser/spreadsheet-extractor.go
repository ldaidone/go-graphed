package parser

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/xuri/excelize/v2"
)

// extractSpreadsheetData dispatches to the format-specific extractor.
// CSV is handled with the standard library; XLSX uses excelize. The
// legacy .xls binary format is not supported and yields no entities
// (the file still appears in the graph as a document).
func extractSpreadsheetData(path string) ([]ir.Entity, error) {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")) {
	case "csv":
		return extractCSVData(path)
	case "xlsx":
		return extractXLSXData(path)
	default:
		return nil, nil
	}
}

// extractCSVData reads a CSV file and emits a single data-table
// entity carrying the row/column counts and header row.
func extractCSVData(path string) ([]ir.Entity, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("unable to open csv: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("unable to parse csv: %w", err)
	}

	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	entity := ir.Entity{
		ID:   fmt.Sprintf("%s#table", path),
		Type: "data-table",
		Name: name,
		Metadata: map[string]string{
			"format":  "csv",
			"rows":    fmt.Sprintf("%d", populatedRowCount(records)),
			"columns": fmt.Sprintf("%d", maxColumnCount(records)),
		},
	}
	if len(records) > 0 {
		entity.Metadata["header"] = strings.Join(records[0], ", ")
	}
	return []ir.Entity{entity}, nil
}

// extractXLSXData reads an XLSX workbook and emits one data-table
// entity per worksheet.
func extractXLSXData(path string) ([]ir.Entity, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to open xlsx: %w", err)
	}
	defer f.Close()

	var entities []ir.Entity
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return nil, fmt.Errorf("unable to read sheet %q: %w", sheet, err)
		}
		entity := ir.Entity{
			ID:   fmt.Sprintf("%s#table:%s", path, sheet),
			Type: "data-table",
			Name: sheet,
			Metadata: map[string]string{
				"format":  "xlsx",
				"sheet":   sheet,
				"rows":    fmt.Sprintf("%d", populatedRowCount(rows)),
				"columns": fmt.Sprintf("%d", maxColumnCount(rows)),
			},
		}
		if len(rows) > 0 {
			entity.Metadata["header"] = strings.Join(rows[0], ", ")
		}
		entities = append(entities, entity)
	}

	return entities, nil
}

// populatedRowCount counts rows containing at least one non-empty cell.
func populatedRowCount(rows [][]string) int {
	count := 0
	for _, row := range rows {
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				count++
				break
			}
		}
	}
	return count
}

// maxColumnCount returns the widest row, in cells.
func maxColumnCount(rows [][]string) int {
	max := 0
	for _, row := range rows {
		if len(row) > max {
			max = len(row)
		}
	}
	return max
}
