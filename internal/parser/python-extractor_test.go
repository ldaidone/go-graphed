package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func parsePython(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "python", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_PythonFile_EmptyFile(t *testing.T) {
	doc := parsePython(t, "empty.py", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty Python file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "python-extractor" {
		t.Errorf("processor = %q, want python-extractor", doc.Metadata["processor"])
	}
}

func TestParse_PythonFile_Imports(t *testing.T) {
	src := `import os
import os.path as osp
from typing import List, Optional
from . import sibling
`
	doc := parsePython(t, "imports.py", src)

	names := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "import" {
			names[e.Name] = true
		}
	}
	for _, want := range []string{"os", "os.path", "typing", "."} {
		if !names[want] {
			t.Errorf("expected import %q, got %v", want, names)
		}
	}
	for _, e := range doc.Entities {
		if e.Type == "import" && (e.ID == "" || e.Name == "") {
			t.Errorf("import entity has empty ID or Name: %+v", e)
		}
	}
}

func TestParse_PythonFile_ClassesAndMethods(t *testing.T) {
	src := `class Shape:
    def area(self):
        return 0

    def helper(self):
        return self.area()

class Circle(Shape):
    def area(self):
        return 3.14
`
	doc := parsePython(t, "classes.py", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["Shape"] != "class" {
		t.Errorf("Shape type = %q, want class", byName["Shape"])
	}
	if byName["Circle"] != "class" {
		t.Errorf("Circle type = %q, want class", byName["Circle"])
	}
	if byName["Shape.area"] != "method" {
		t.Errorf("Shape.area type = %q, want method", byName["Shape.area"])
	}
	if byName["Shape.helper"] != "method" {
		t.Errorf("Shape.helper type = %q, want method", byName["Shape.helper"])
	}
	if byName["Circle.area"] != "method" {
		t.Errorf("Circle.area type = %q, want method", byName["Circle.area"])
	}
	if byName["area"] != "" {
		t.Errorf("method should not be registered under bare name, got %q", byName["area"])
	}
}

func TestParse_PythonFile_FunctionsAndCalls(t *testing.T) {
	src := `def helper():
    return 42

def top():
    return helper()

def unused():
    return 0
`
	doc := parsePython(t, "calls.py", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["helper"] != "function" {
		t.Errorf("helper type = %q, want function", byName["helper"])
	}
	if byName["top"] != "function" {
		t.Errorf("top type = %q, want function", byName["top"])
	}

	topID := ""
	for _, e := range doc.Entities {
		if e.Name == "top" {
			topID = e.ID
		}
	}
	if topID == "" {
		t.Fatal("expected a function entity named top")
	}

	found := false
	for _, l := range doc.Links {
		if l.Type == "calls" && l.SourceID == topID {
			if !strings.HasSuffix(l.TargetID, "#function:helper") {
				t.Errorf("top call target = %q, want ...#function:helper", l.TargetID)
			}
			if l.Weight != 1.0 {
				t.Errorf("call weight = %v, want 1.0", l.Weight)
			}
			if l.SourceType != ir.LinkSourceExtracted {
				t.Errorf("call source type = %q, want %q", l.SourceType, ir.LinkSourceExtracted)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("expected a calls link from top to helper, got %v", doc.Links)
	}

	// A method call through self is an attribute call and must not
	// resolve within the file.
}

func TestParse_PythonFile_DecoratedDefinition(t *testing.T) {
	src := `@dataclass
class Point:
    x: int

    def distance(self):
        return 0
`
	doc := parsePython(t, "decorated.py", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["Point"] != "class" {
		t.Errorf("Point type = %q, want class (decorated_definition must be unwrapped)", byName["Point"])
	}
	if byName["Point.distance"] != "method" {
		t.Errorf("Point.distance type = %q, want method", byName["Point.distance"])
	}
}

func TestParse_PythonFile_AsyncFunctions(t *testing.T) {
	src := `async def fetch():
    return 1

def run():
    return fetch()
`
	doc := parsePython(t, "async.py", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	if byName["fetch"] != "function" {
		t.Errorf("async fetch type = %q, want function", byName["fetch"])
	}
	if byName["run"] != "function" {
		t.Errorf("run type = %q, want function", byName["run"])
	}
}

func TestParse_PythonFile_NoMethodCallResolution(t *testing.T) {
	src := `class Service:
    def helper(self):
        return 1

    def run(self):
        return self.helper()
`
	doc := parsePython(t, "method_calls.py", src)

	for _, l := range doc.Links {
		if l.Type == "calls" {
			t.Errorf("self.helper() is an attribute call and must not resolve, got link %+v", l)
		}
	}
}

func TestParse_PythonFile_SyntacticallyInvalid(t *testing.T) {
	src := `def broken(:
    this is not valid python
    {{{
`
	doc := parsePython(t, "invalid.py", src)

	// Tree-sitter may or may not recover declarations.  The important
	// invariant is that no entity has empty ID or Name.
	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}
