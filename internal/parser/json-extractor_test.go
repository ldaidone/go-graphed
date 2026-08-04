package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestParse_JSONFile(t *testing.T) {
	tmp := t.TempDir()
	src := `{
  "name": "my-app",
  "version": "1.0.0",
  "scripts": {
    "build": "tsc",
    "test": "jest"
  },
  "dependencies": {
    "lodash": "^4.17.21"
  }
}
`
	path := filepath.Join(tmp, "package.json")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "json", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Format != "json" {
		t.Errorf("Format = %q, want %q", doc.Format, "json")
	}
	if doc.Metadata["processor"] != "json-extractor" {
		t.Errorf("Metadata[processor] = %q, want %q", doc.Metadata["processor"], "json-extractor")
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	// Top-level keys.
	if byName["name"] != "property" {
		t.Errorf("name type = %q, want property", byName["name"])
	}
	if byName["version"] != "property" {
		t.Errorf("version type = %q, want property", byName["version"])
	}

	// Nested keys use dot paths for uniqueness.
	if byName["scripts.build"] != "property" {
		t.Errorf("scripts.build type = %q, want property", byName["scripts.build"])
	}
	if byName["dependencies.lodash"] != "property" {
		t.Errorf("dependencies.lodash type = %q, want property", byName["dependencies.lodash"])
	}

	// Container-valued pairs become object entities, not properties.
	if byName["scripts"] != "object" {
		t.Errorf("scripts type = %q, want object", byName["scripts"])
	}
	if byName["dependencies"] != "object" {
		t.Errorf("dependencies type = %q, want object", byName["dependencies"])
	}

	if len(doc.Entities) != 7 {
		t.Errorf("expected 7 entities, got %d: %v", len(doc.Entities), byName)
	}
}

func TestParse_JSONFile_Arrays(t *testing.T) {
	tmp := t.TempDir()
	src := `{
  "keywords": ["graph", "cli"],
  "files": [
    "dist",
    "README.md"
  ]
}
`
	path := filepath.Join(tmp, "config.json")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "json", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["keywords"] != "array" {
		t.Errorf("keywords type = %q, want array", byName["keywords"])
	}
	if byName["files"] != "array" {
		t.Errorf("files type = %q, want array", byName["files"])
	}
}

func TestParse_JSONFile_Empty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.json")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "json", Size: 2}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(doc.Entities))
	}
}
