package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestParse_GoFile_FunctionsMethodsImports(t *testing.T) {
	tmp := t.TempDir()
	src := `package demo

import (
	"fmt"
)

type Dog struct{}

func (d *Dog) Speak() string { return "woof" }

func Greet(d *Dog) string {
	s := d.Speak()
	help(s)
	return fmt.Sprintf("%s", s)
}

func help(s string) string { return s }
`
	path := filepath.Join(tmp, "demo.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "golang", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	// Package clause.
	if byName["demo"] != "package" {
		t.Errorf("demo type = %q, want package", byName["demo"])
	}
	// Struct and interface types (existing behavior).
	if byName["Dog"] != "struct" {
		t.Errorf("Dog type = %q, want struct", byName["Dog"])
	}
	// Functions.
	if byName["Greet"] != "function" {
		t.Errorf("Greet type = %q, want function", byName["Greet"])
	}
	if byName["help"] != "function" {
		t.Errorf("help type = %q, want function", byName["help"])
	}
	// Method named with its receiver.
	if byName["Dog.Speak"] != "method" {
		t.Errorf("Dog.Speak type = %q, want method", byName["Dog.Speak"])
	}
	// Import.
	if byName["fmt"] != "import" {
		t.Errorf("fmt type = %q, want import", byName["fmt"])
	}

	// package + struct + 2 functions + method + import.
	if len(doc.Entities) != 6 {
		t.Errorf("expected 6 entities, got %d: %v", len(doc.Entities), byName)
	}

	// Greet calls help; selector calls (d.Speak, fmt.Sprintf) do not
	// produce within-file links.
	if len(doc.Links) != 1 {
		t.Fatalf("expected 1 calls link, got %d", len(doc.Links))
	}
	link := doc.Links[0]
	if link.Type != "calls" {
		t.Errorf("link Type = %q, want calls", link.Type)
	}
	wantSource := path + "#function:Greet"
	wantTarget := path + "#function:help"
	if link.SourceID != wantSource {
		t.Errorf("link SourceID = %q, want %q", link.SourceID, wantSource)
	}
	if link.TargetID != wantTarget {
		t.Errorf("link TargetID = %q, want %q", link.TargetID, wantTarget)
	}
	if link.SourceType != ir.LinkSourceExtracted {
		t.Errorf("link SourceType = %q, want %q", link.SourceType, ir.LinkSourceExtracted)
	}
}

func TestParse_GoFile_CallGraphSurvivesAnalyzer(t *testing.T) {
	tmp := t.TempDir()
	src := `package demo

func alpha() { beta() }

func beta() {}
`
	path := filepath.Join(tmp, "call.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "golang", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	// Forward reference: alpha calls beta, declared later in the file.
	if len(doc.Links) != 1 {
		t.Fatalf("expected 1 calls link, got %d", len(doc.Links))
	}
	if doc.Links[0].Type != "calls" {
		t.Errorf("link Type = %q, want calls", doc.Links[0].Type)
	}
}
