package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestParse_Makefile(t *testing.T) {
	tmp := t.TempDir()
	src := "CC=gcc\nall: build\n\t$(CC) -o out main.c\nbuild:\n\t@echo ok\n.PHONY: clean\nclean:\n\trm -f out\n"
	path := filepath.Join(tmp, "Makefile")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "make", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Metadata["processor"] != "make-extractor" {
		t.Errorf("Metadata[processor] = %q, want make-extractor", doc.Metadata["processor"])
	}

	byName := map[string]string{}
	byValue := map[string]string{}
	byPrereqs := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
		if v, ok := e.Metadata["value"]; ok {
			byValue[e.Name] = v
		}
		if p, ok := e.Metadata["prerequisites"]; ok {
			byPrereqs[e.Name] = p
		}
	}

	if byName["CC"] != "variable" {
		t.Errorf("CC type = %q, want variable", byName["CC"])
	}
	if byValue["CC"] != "gcc" {
		t.Errorf("CC value = %q, want gcc", byValue["CC"])
	}
	if byName["all"] != "target" {
		t.Errorf("all type = %q, want target", byName["all"])
	}
	if byPrereqs["all"] != "build" {
		t.Errorf("all prerequisites = %q, want build", byPrereqs["all"])
	}
	if byName["build"] != "target" {
		t.Errorf("build type = %q, want target", byName["build"])
	}
	if byName["clean"] != "target" {
		t.Errorf("clean type = %q, want target", byName["clean"])
	}
	// Directives like .PHONY must not become targets.
	if _, ok := byName[".PHONY"]; ok {
		t.Errorf(".PHONY should not be an entity: %v", byName)
	}

	if len(doc.Entities) != 4 {
		t.Errorf("expected 4 entities, got %d: %v", len(doc.Entities), byName)
	}
}
