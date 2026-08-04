package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestParse_YAMLFile(t *testing.T) {
	tmp := t.TempDir()
	src := `name: my-app
version: 1.0
scripts:
  build: tsc
  list:
    - a
    - b
`
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "yaml", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Metadata["processor"] != "yaml-extractor" {
		t.Errorf("Metadata[processor] = %q, want yaml-extractor", doc.Metadata["processor"])
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["name"] != "property" {
		t.Errorf("name type = %q, want property", byName["name"])
	}
	if byName["version"] != "property" {
		t.Errorf("version type = %q, want property", byName["version"])
	}
	if byName["scripts"] != "object" {
		t.Errorf("scripts type = %q, want object", byName["scripts"])
	}
	if byName["scripts.build"] != "property" {
		t.Errorf("scripts.build type = %q, want property", byName["scripts.build"])
	}
	if byName["scripts.list"] != "array" {
		t.Errorf("scripts.list type = %q, want array", byName["scripts.list"])
	}
	if byName["a"] != "sequence-item" {
		t.Errorf("sequence item a type = %q, want sequence-item", byName["a"])
	}
	if byName["b"] != "sequence-item" {
		t.Errorf("sequence item b type = %q, want sequence-item", byName["b"])
	}

	if len(doc.Entities) != 7 {
		t.Errorf("expected 7 entities, got %d: %v", len(doc.Entities), byName)
	}
}

func TestParse_YAMLFile_QuotedKeys(t *testing.T) {
	tmp := t.TempDir()
	src := `"quoted key": value
'single': 1
`
	path := filepath.Join(tmp, "quoted.yaml")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "yaml", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["quoted key"] != "property" {
		t.Errorf("quoted key type = %q, want property", byName["quoted key"])
	}
	if byName["single"] != "property" {
		t.Errorf("single-quoted key type = %q, want property", byName["single"])
	}
}
