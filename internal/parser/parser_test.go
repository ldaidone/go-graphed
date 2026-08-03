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

func TestParse_MarkdownFile(t *testing.T) {
	tmp := t.TempDir()
	src := `# Title

## Section One

See [the docs](https://example.com/docs) and the [reference][ref].

![logo](assets/logo.png)

[ref]: https://example.com/reference
`
	path := filepath.Join(tmp, "guide.md")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "markdown", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Format != "markdown" {
		t.Errorf("Format = %q, want %q", doc.Format, "markdown")
	}
	if doc.Metadata["processor"] != "markdown-extractor" {
		t.Errorf("Metadata[processor] = %q, want %q", doc.Metadata["processor"], "markdown-extractor")
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	// Headings.
	if byName["Title"] != "heading" {
		t.Errorf("Title entity type = %q, want heading", byName["Title"])
	}
	if byName["Section One"] != "heading" {
		t.Errorf("Section One entity type = %q, want heading", byName["Section One"])
	}

	// Inline link + reference definition.
	if byName["the docs"] != "link" {
		t.Errorf("inline link entity type = %q, want link", byName["the docs"])
	}
	if byName["ref"] != "link" {
		t.Errorf("reference definition entity type = %q, want link", byName["ref"])
	}

	// Image (alt text is the name) -- must not be duplicated as a link.
	if byName["logo"] != "image-asset" {
		t.Errorf("image entity type = %q, want image-asset", byName["logo"])
	}

	for _, e := range doc.Entities {
		if e.Type == "link" && e.Name == "logo" {
			t.Errorf("image was also extracted as a plain link")
		}
	}
}

func TestParse_MarkdownTreeSitter(t *testing.T) {
	tmp := t.TempDir()
	src := `# Title

Some setext heading
===================

See [inline](https://example.com/inline), the [full][ref], and the [shortcut].

![diagram](assets/diagram.png)

[ref]: https://example.com/reference

    [fake](https://example.com/indented)

` + "```" + `go
	link := "[not-a-link](https://example.com/fake)"
` + "```" + `
`
	path := filepath.Join(tmp, "treesitter.md")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "markdown", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byName := map[string]string{}
	byTarget := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
		if e.Metadata != nil {
			if tgt, ok := e.Metadata["target"]; ok {
				byTarget[tgt] = e.Type
			}
		}
	}

	// Setext headings carry a level and are still headings.
	if byName["Some setext heading"] != "heading" {
		t.Errorf("setext heading type = %q, want heading", byName["Some setext heading"])
	}

	// Full and shortcut reference forms are links, with the label as target.
	if byName["full"] != "link" {
		t.Errorf("full reference link type = %q, want link", byName["full"])
	}
	if byTarget["ref"] != "link" {
		t.Errorf("full reference link target = %q, want ref", byTarget["ref"])
	}
	if byName["shortcut"] != "link" {
		t.Errorf("shortcut link type = %q, want link", byName["shortcut"])
	}

	// Nothing inside a code fence or indented block may leak out as a link.
	if _, ok := byName["not-a-link"]; ok {
		t.Errorf("link inside fenced code block was extracted: %v", byName)
	}
	if _, ok := byName["fake"]; ok {
		t.Errorf("link inside indented block was extracted: %v", byName)
	}

	// Level metadata is set on ATX headings.
	for _, e := range doc.Entities {
		if e.Name == "Title" {
			if e.Metadata["level"] != "1" {
				t.Errorf("Title level = %q, want 1", e.Metadata["level"])
			}
		}
	}
	for _, e := range doc.Entities {
		if e.Name == "Some setext heading" {
			if e.Metadata["level"] != "1" {
				t.Errorf("setext heading level = %q, want 1", e.Metadata["level"])
			}
		}
	}
}
