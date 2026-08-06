package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parseRust(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "rust", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func nameByID(doc ir.Document) map[string]string {
	out := map[string]string{}
	for _, e := range doc.Entities {
		out[e.ID] = e.Name
	}
	return out
}

func TestParse_RustFile_EmptyFile(t *testing.T) {
	doc := parseRust(t, "empty.rs", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty Rust file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "rust-extractor" {
		t.Errorf("processor = %q, want rust-extractor", doc.Metadata["processor"])
	}
}

func TestParse_RustFile_Imports(t *testing.T) {
	src := `use std::collections::{HashMap, HashSet};
use crate::models;
use super::helper;
`
	doc := parseRust(t, "imports.rs", src)

	names := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "import" {
			names[e.Name] = true
		}
	}
	for _, want := range []string{"std::collections", "crate::models", "super::helper"} {
		if !names[want] {
			t.Errorf("expected import %q, got %v", want, names)
		}
	}
}

func TestParse_RustFile_Declarations(t *testing.T) {
	src := `mod helpers;

struct Point {
    x: f64,
}

enum Shape {
    Circle(f64),
}

trait Area {
    fn area(&self) -> f64;
}
`
	doc := parseRust(t, "decls.rs", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["helpers"] != "module" {
		t.Errorf("helpers type = %q, want module", byName["helpers"])
	}
	if byName["Point"] != "struct" {
		t.Errorf("Point type = %q, want struct", byName["Point"])
	}
	if byName["Shape"] != "enum" {
		t.Errorf("Shape type = %q, want enum", byName["Shape"])
	}
	if byName["Area"] != "trait" {
		t.Errorf("Area type = %q, want trait", byName["Area"])
	}
	if byName["Area.area"] != "method" {
		t.Errorf("Area.area type = %q, want method", byName["Area.area"])
	}
}

func TestParse_RustFile_ImplMethods(t *testing.T) {
	src := `impl Point {
    fn new(x: f64) -> Point {
        Point { x, y: 0.0 }
    }
}

impl Area for Point {
    fn area(&self) -> f64 {
        0.0
    }
}
`
	doc := parseRust(t, "impls.rs", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["Point.new"] != "method" {
		t.Errorf("Point.new type = %q, want method", byName["Point.new"])
	}
	if byName["Point.area"] != "method" {
		t.Errorf("Point.area type = %q, want method", byName["Point.area"])
	}
	if byName["new"] != "" {
		t.Errorf("impl methods must not register under bare name, got %q", byName["new"])
	}
	// impl blocks emit no entity themselves.
	for _, e := range doc.Entities {
		if e.Type == "impl" {
			t.Errorf("impl_item must not emit an entity, got %+v", e)
		}
	}
}

func TestParse_RustFile_FunctionsAndCalls(t *testing.T) {
	src := `fn helper() -> i32 {
    42
}

fn compute(p: &Point) -> i32 {
    helper()
}
`
	doc := parseRust(t, "calls.rs", src)

	computeID := ""
	for _, e := range doc.Entities {
		if e.Name == "compute" {
			computeID = e.ID
		}
	}
	if computeID == "" {
		t.Fatal("expected a function entity named compute")
	}

	found := false
	for _, l := range doc.Links {
		if l.Type == "calls" && l.SourceID == computeID {
			if !strings.HasSuffix(l.TargetID, "#function:helper") {
				t.Errorf("compute call target = %q, want ...#function:helper", l.TargetID)
			}
			if l.SourceType != ir.LinkSourceExtracted {
				t.Errorf("call source type = %q, want %q", l.SourceType, ir.LinkSourceExtracted)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("expected a calls link from compute to helper, got %v", doc.Links)
	}
}

func TestParse_RustFile_NoQualifiedCallResolution(t *testing.T) {
	src := `fn helper() {}

fn compute(p: &Point) {
    helper();
    Point::new(1.0, 2.0);
    p.area();
}
`
	doc := parseRust(t, "qualified.rs", src)

	computeID := ""
	for _, e := range doc.Entities {
		if e.Name == "compute" {
			computeID = e.ID
		}
	}
	if computeID == "" {
		t.Fatal("expected a function entity named compute")
	}

	seen := 0
	for _, l := range doc.Links {
		if l.SourceID == computeID {
			seen++
			if !strings.HasSuffix(l.TargetID, "#function:helper") {
				t.Errorf("qualified call %q -> %q must not resolve", l.SourceID, l.TargetID)
			}
		}
	}
	if seen != 1 {
		t.Errorf("expected exactly 1 calls link (helper), got %d: %v", seen, doc.Links)
	}
}

func TestParse_RustFile_SyntacticallyInvalid(t *testing.T) {
	src := `fn broken( {
    this is not valid rust
}
`
	doc := parseRust(t, "invalid.rs", src)
	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}
