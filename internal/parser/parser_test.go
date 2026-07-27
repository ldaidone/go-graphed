package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestParse_GoFile(t *testing.T) {
	// Write a minimal Go source file with a struct and an interface.
	tmp := t.TempDir()
	src := `package test

type Animal interface {
	Speak() string
}

type Dog struct {
	Name string
}
`
	path := filepath.Join(tmp, "types.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "golang", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Format != "golang" {
		t.Errorf("Format = %q, want %q", doc.Format, "golang")
	}
	if doc.Path != path {
		t.Errorf("Path = %q, want %q", doc.Path, path)
	}
	if len(doc.Entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(doc.Entities))
	}

	// Verify entity names and types (order may vary).
	byName := make(map[string]string)
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["Animal"] != "interface" {
		t.Errorf("Animal type = %q, want %q", byName["Animal"], "interface")
	}
	if byName["Dog"] != "struct" {
		t.Errorf("Dog type = %q, want %q", byName["Dog"], "struct")
	}
}

func TestParse_GoFile_NoEntities(t *testing.T) {
	tmp := t.TempDir()
	src := `package empty
`
	path := filepath.Join(tmp, "empty.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "golang", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(doc.Entities))
	}
}

func TestParse_GoFile_BadPath(t *testing.T) {
	file := scanner.File{Path: "/nonexistent/file.go", Language: "golang", Size: 0}
	_, err := Parse(file)
	if err == nil {
		t.Error("Parse with bad path should return an error")
	}
}

func TestParse_FallbackLanguages(t *testing.T) {
	tests := []struct {
		name        string
		language    string
		expectedKey string
	}{
		{
			name:        "pdf sets fallback processor",
			language:    "pdf",
			expectedKey: "pdf-fallback-extractor",
		},
		{
			name:        "spreadsheet sets fallback processor",
			language:    "spreadsheet",
			expectedKey: "excel-fallback-extractor",
		},
		{
			name:        "markdown sets fallback processor",
			language:    "markdown",
			expectedKey: "markdown-fallback-extractor",
		},
		{
			name:        "unknown language sets generic processor",
			language:    "plaintext",
			expectedKey: "generic-unstructured-extractor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := scanner.File{Path: "dummy", Language: tt.language, Size: 0}
			doc, err := Parse(file)
			if err != nil {
				t.Fatalf("Parse returned unexpected error: %v", err)
			}
			got := doc.Metadata["processor"]
			if got != tt.expectedKey {
				t.Errorf("Metadata[processor] = %q, want %q", got, tt.expectedKey)
			}
			if len(doc.Entities) != 0 {
				t.Errorf("expected 0 entities for fallback language, got %d", len(doc.Entities))
			}
		})
	}
}
