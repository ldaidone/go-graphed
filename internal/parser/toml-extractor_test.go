package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestParse_TOMLFile(t *testing.T) {
	tmp := t.TempDir()
	src := `[build]
name = "app"

[server.port]
port = 8080
`
	path := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "toml", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Metadata["processor"] != "toml-extractor" {
		t.Errorf("Metadata[processor] = %q, want toml-extractor", doc.Metadata["processor"])
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["build"] != "table" {
		t.Errorf("build type = %q, want table", byName["build"])
	}
	if byName["build.name"] != "property" {
		t.Errorf("build.name type = %q, want property", byName["build.name"])
	}
	if byName["server.port"] != "table" {
		t.Errorf("server.port type = %q, want table", byName["server.port"])
	}
	if byName["server.port.port"] != "property" {
		t.Errorf("server.port.port type = %q, want property", byName["server.port.port"])
	}

	if len(doc.Entities) != 4 {
		t.Errorf("expected 4 entities, got %d: %v", len(doc.Entities), byName)
	}
}

func TestParse_TOMLFile_RootPairsAndArrays(t *testing.T) {
	tmp := t.TempDir()
	src := `title = "demo"
ports = [8080, 9090]
`
	path := filepath.Join(tmp, "root.toml")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "toml", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["title"] != "property" {
		t.Errorf("title type = %q, want property", byName["title"])
	}
	if byName["ports"] != "array" {
		t.Errorf("ports type = %q, want array", byName["ports"])
	}
}
