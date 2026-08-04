package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/scanner"
	"github.com/xuri/excelize/v2"
)

func TestParse_CSVFile(t *testing.T) {
	tmp := t.TempDir()
	src := "name,age,city\nalice,30,nyc\nbob,25,sf\n"
	path := filepath.Join(tmp, "people.csv")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "spreadsheet", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Metadata["processor"] != "spreadsheet-extractor" {
		t.Errorf("Metadata[processor] = %q, want spreadsheet-extractor", doc.Metadata["processor"])
	}

	if len(doc.Entities) != 1 {
		t.Fatalf("expected 1 data-table entity, got %d", len(doc.Entities))
	}
	e := doc.Entities[0]
	if e.Type != "data-table" {
		t.Errorf("entity type = %q, want data-table", e.Type)
	}
	if e.Name != "people" {
		t.Errorf("entity name = %q, want people", e.Name)
	}
	if e.Metadata["rows"] != "3" {
		t.Errorf("rows = %q, want 3", e.Metadata["rows"])
	}
	if e.Metadata["columns"] != "3" {
		t.Errorf("columns = %q, want 3", e.Metadata["columns"])
	}
	if e.Metadata["header"] != "name, age, city" {
		t.Errorf("header = %q, want %q", e.Metadata["header"], "name, age, city")
	}
}

func TestParse_XLSXFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "budget.xlsx")

	book := excelize.NewFile()
	sheet := "Data"
	book.SetSheetName(book.GetSheetName(0), sheet)
	_ = book.SetCellValue(sheet, "A1", "item")
	_ = book.SetCellValue(sheet, "B1", "amount")
	_ = book.SetCellValue(sheet, "A2", "hosting")
	_ = book.SetCellValue(sheet, "B2", 100)
	if err := book.SaveAs(path); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "spreadsheet", Size: 0}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Metadata["processor"] != "spreadsheet-extractor" {
		t.Errorf("Metadata[processor] = %q, want spreadsheet-extractor", doc.Metadata["processor"])
	}

	if len(doc.Entities) != 1 {
		t.Fatalf("expected 1 data-table entity, got %d", len(doc.Entities))
	}
	e := doc.Entities[0]
	if e.Type != "data-table" {
		t.Errorf("entity type = %q, want data-table", e.Type)
	}
	if e.Name != "Data" {
		t.Errorf("entity name = %q, want Data", e.Name)
	}
	if e.Metadata["format"] != "xlsx" {
		t.Errorf("format = %q, want xlsx", e.Metadata["format"])
	}
	if e.Metadata["rows"] != "2" {
		t.Errorf("rows = %q, want 2", e.Metadata["rows"])
	}
	if e.Metadata["columns"] != "2" {
		t.Errorf("columns = %q, want 2", e.Metadata["columns"])
	}
}
