package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestParse_JSFile(t *testing.T) {
	tmp := t.TempDir()
	src := `import x from 'y';
function foo(a) { return bar(a); }
const baz = (p) => p;
export const c = 1;
class A { m() { return foo(); } }
`
	path := filepath.Join(tmp, "app.js")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "javascript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Metadata["processor"] != "js-extractor" {
		t.Errorf("Metadata[processor] = %q, want js-extractor", doc.Metadata["processor"])
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["y"] != "import" {
		t.Errorf("import y type = %q, want import", byName["y"])
	}
	if byName["foo"] != "function" {
		t.Errorf("foo type = %q, want function", byName["foo"])
	}
	if byName["baz"] != "function" {
		t.Errorf("arrow function baz type = %q, want function", byName["baz"])
	}
	if byName["A"] != "class" {
		t.Errorf("A type = %q, want class", byName["A"])
	}
	if byName["A.m"] != "method" {
		t.Errorf("method A.m type = %q, want method", byName["A.m"])
	}

	// Plain consts are not functions.
	if _, ok := byName["c"]; ok {
		t.Errorf("const c should not be an entity: %v", byName)
	}

	if len(doc.Entities) != 5 {
		t.Errorf("expected 5 entities, got %d: %v", len(doc.Entities), byName)
	}
}
