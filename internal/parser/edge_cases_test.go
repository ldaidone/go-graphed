package parser

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestUnquoteJSONKey_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain quoted key", in: `"key"`, want: "key"},
		{name: "unquoted key", in: "key", want: "key"},
		{name: "whitespace trimmed", in: `  "pad"  `, want: "pad"},
		{name: "empty quoted key", in: `""`, want: ""},
		{name: "escape sequences resolved", in: `"a\nb"`, want: "a\nb"},
		{name: "invalid escape kept", in: `"bad\q"`, want: `"bad\q"`},
		{name: "single quotes untouched", in: `'single'`, want: `'single'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unquoteJSONKey(tt.in); got != tt.want {
				t.Errorf("unquoteJSONKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestUnquoteString_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "double quoted", in: `"double"`, want: "double"},
		{name: "single quoted", in: `'single'`, want: "single"},
		{name: "escaped single quote", in: `'it\'s'`, want: "it's"},
		{name: "escaped double quote resolved", in: `"a\nb"`, want: "a\nb"},
		{name: "invalid escape kept", in: `"bad\q"`, want: `"bad\q"`},
		{name: "unquoted string", in: "plain", want: "plain"},
		{name: "whitespace trimmed", in: `  "pad"  `, want: "pad"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unquoteString(tt.in); got != tt.want {
				t.Errorf("unquoteString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestUnquoteYAMLScalar_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain scalar", in: "value", want: "value"},
		{name: "double quoted", in: `"value"`, want: "value"},
		{name: "single quoted", in: `'value'`, want: "value"},
		{name: "whitespace trimmed", in: `  "value"  `, want: "value"},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unquoteYAMLScalar(tt.in); got != tt.want {
				t.Errorf("unquoteYAMLScalar(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPopulatedRowCount_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		rows [][]string
		want int
	}{
		{name: "all populated", rows: [][]string{{"a", "b"}, {"c", "d"}}, want: 2},
		{name: "blank row skipped", rows: [][]string{{"a"}, {" ", "  "}, {"c"}}, want: 2},
		{name: "empty", rows: [][]string{}, want: 0},
		{name: "nil rows", rows: nil, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := populatedRowCount(tt.rows); got != tt.want {
				t.Errorf("populatedRowCount(%v) = %d, want %d", tt.rows, got, tt.want)
			}
		})
	}
}

func TestMaxColumnCount_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		rows [][]string
		want int
	}{
		{name: "ragged rows", rows: [][]string{{"a", "b"}, {"c"}}, want: 2},
		{name: "equal width", rows: [][]string{{"a", "b"}, {"c", "d"}}, want: 2},
		{name: "empty", rows: [][]string{}, want: 0},
		{name: "nil rows", rows: nil, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maxColumnCount(tt.rows); got != tt.want {
				t.Errorf("maxColumnCount(%v) = %d, want %d", tt.rows, got, tt.want)
			}
		})
	}
}

func TestParse_ErrorPaths_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		language string
		path     string
		wantErr  string
	}{
		{name: "golang", language: "golang", path: "/nonexistent/main.go", wantErr: "golang parser failed"},
		{name: "json", language: "json", path: "/nonexistent/config.json", wantErr: "json parser failed"},
		{name: "yaml", language: "yaml", path: "/nonexistent/cfg.yaml", wantErr: "yaml parser failed"},
		{name: "toml", language: "toml", path: "/nonexistent/cfg.toml", wantErr: "toml parser failed"},
		{name: "javascript", language: "javascript", path: "/nonexistent/app.js", wantErr: "javascript parser failed"},
		{name: "typescript", language: "typescript", path: "/nonexistent/app.ts", wantErr: "typescript parser failed"},
		{name: "tsx", language: "tsx", path: "/nonexistent/app.tsx", wantErr: "typescript parser failed"},
		{name: "dockerfile", language: "dockerfile", path: "/nonexistent/Dockerfile", wantErr: "dockerfile parser failed"},
		{name: "make", language: "make", path: "/nonexistent/Makefile", wantErr: "make parser failed"},
		{name: "markdown", language: "markdown", path: "/nonexistent/readme.md", wantErr: "markdown parser failed"},
		{name: "pdf", language: "pdf", path: "/nonexistent/doc.pdf", wantErr: "pdf parser failed"},
		{name: "spreadsheet", language: "spreadsheet", path: "/nonexistent/data.csv", wantErr: "spreadsheet parser failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := scanner.File{Path: tt.path, Language: tt.language}
			_, err := Parse(file)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestExtractSpreadsheetData_Dispatch(t *testing.T) {
	t.Run("legacy xls yields no entities", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "legacy.xls")
		if err := os.WriteFile(path, []byte("not really xls"), 0644); err != nil {
			t.Fatal(err)
		}
		entities, err := extractSpreadsheetData(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entities != nil {
			t.Errorf("expected nil entities for .xls, got %v", entities)
		}
	})

	t.Run("malformed csv errors", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.csv")
		if err := os.WriteFile(path, []byte("a,b\n\"unterminated"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := extractSpreadsheetData(path); err == nil {
			t.Error("expected error for malformed CSV")
		}
	})

	t.Run("malformed xlsx errors", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.xlsx")
		if err := os.WriteFile(path, []byte("not a zip"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := extractSpreadsheetData(path); err == nil {
			t.Error("expected error for malformed XLSX")
		}
	})
}

func TestParse_JSONFile_NestedObjectsInArrays(t *testing.T) {
	tmp := t.TempDir()
	src := `{
  "items": [
    {"name": "a"},
    {"name": "b"}
  ]
}
`
	path := filepath.Join(tmp, "nested.json")
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
	if byName["items"] != "array" {
		t.Errorf("items type = %q, want array", byName["items"])
	}
	// Both objects inside the array carry the same key path in the current
	// implementation; verify the nested property was still extracted.
	if byName["items.name"] != "property" {
		t.Errorf("items.name type = %q, want property", byName["items.name"])
	}
}

func TestParse_YAMLFile_FlowAndSequences(t *testing.T) {
	tmp := t.TempDir()
	src := `scalar: value
flow_map: {a: 1, b: 2}
flow_seq: [x, y]
list:
  - name: one
  - name: two
empty_map: {}
'single quoted key': 5
`
	path := filepath.Join(tmp, "flow.yaml")
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
	if byName["flow_map"] != "object" {
		t.Errorf("flow_map type = %q, want object", byName["flow_map"])
	}
	if byName["flow_seq"] != "array" {
		t.Errorf("flow_seq type = %q, want array", byName["flow_seq"])
	}
	if byName["list"] != "array" {
		t.Errorf("list type = %q, want array", byName["list"])
	}
	if byName["list.0.name"] != "property" {
		t.Errorf("list.0.name type = %q, want property", byName["list.0.name"])
	}
	if byName["list.1.name"] != "property" {
		t.Errorf("list.1.name type = %q, want property", byName["list.1.name"])
	}
	if byName["empty_map"] != "object" {
		t.Errorf("empty_map type = %q, want object", byName["empty_map"])
	}
	if byName["single quoted key"] != "property" {
		t.Errorf("single quoted key type = %q, want property", byName["single quoted key"])
	}
}

func TestParse_YAMLFile_EmptyDocument(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.yaml")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "yaml", Size: 0}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty YAML, got %d", len(doc.Entities))
	}
}

func TestParse_TOMLFile_EmptyKey(t *testing.T) {
	// "=100" and "[]" are recovered by the TOML grammar as a pair/table
	// with no key; the extractor must skip them instead of emitting a
	// blank-named entity. Surfaced by the FuzzExtractTOML property test.
	for _, src := range []string{"=100", "[]"} {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "orphan.toml")
		if err := os.WriteFile(path, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}

		file := scanner.File{Path: path, Language: "toml", Size: int64(len(src))}
		doc, err := Parse(file)
		if err != nil {
			t.Fatalf("Parse(%q) returned unexpected error: %v", src, err)
		}
		for _, e := range doc.Entities {
			if e.Name == "" || e.ID == "" {
				t.Errorf("Parse(%q): found entity with empty ID/Name: %+v", src, e)
			}
		}
	}
}

func TestParse_MarkdownFile_EmptyHeading(t *testing.T) {
	// A bare "#" is a heading with no text; the extractor must skip it.
	// Surfaced by the FuzzExtractMarkdown property test.
	tmp := t.TempDir()
	src := "#\n"
	path := filepath.Join(tmp, "bare.md")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "markdown", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	for _, e := range doc.Entities {
		if e.Type == "heading" {
			t.Errorf("expected no heading entities for bare '#', got %+v", e)
		}
	}
}

func TestParse_MakefileFile_EmptyName(t *testing.T) {
	// A lone ":" is recovered by the Make grammar as a rule with an empty
	// target; the extractor must skip it instead of emitting a blank-named
	// entity. Surfaced by the FuzzExtractMakefile property test.
	for _, src := range []string{":\n", "\n:\n"} {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "orphan.mk")
		if err := os.WriteFile(path, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}

		file := scanner.File{Path: path, Language: "makefile", Size: int64(len(src))}
		doc, err := Parse(file)
		if err != nil {
			t.Fatalf("Parse(%q) returned unexpected error: %v", src, err)
		}
		for _, e := range doc.Entities {
			if e.Name == "" || e.ID == "" {
				t.Errorf("Parse(%q): found entity with empty ID/Name: %+v", src, e)
			}
		}
	}
}

func TestParse_TOMLFile_DottedAndQuotedKeys(t *testing.T) {
	tmp := t.TempDir()
	src := `[server]
host = "localhost"
"quoted key" = 1

[a.b.c]
x = 2
`
	path := filepath.Join(tmp, "cfg.toml")
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
	if byName["server"] != "table" {
		t.Errorf("server type = %q, want table", byName["server"])
	}
	if byName["a.b.c"] != "table" {
		t.Errorf("a.b.c type = %q, want table", byName["a.b.c"])
	}
	if byName["server.host"] != "property" {
		t.Errorf("server.host type = %q, want property", byName["server.host"])
	}
	if byName["a.b.c.x"] != "property" {
		t.Errorf("a.b.c.x type = %q, want property", byName["a.b.c.x"])
	}
	// Quoted keys survive with their quotes in the raw name; assert the
	// prefix to stay robust to that detail.
	foundQuoted := false
	for name := range byName {
		if strings.HasPrefix(name, "server.") && strings.Contains(name, "quoted key") {
			foundQuoted = true
		}
	}
	if !foundQuoted {
		t.Errorf("expected a server.* entity for the quoted key, got: %v", reflect.ValueOf(byName).MapKeys())
	}
}

func TestParse_Markdown_AllHeadingLevels(t *testing.T) {
	tmp := t.TempDir()
	src := `# One
## Two
### Three
#### Four
##### Five
###### Six

Setext Two
----------
`
	path := filepath.Join(tmp, "levels.md")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "markdown", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	levels := map[string]string{}
	for _, e := range doc.Entities {
		if e.Type == "heading" {
			levels[e.Name] = e.Metadata["level"]
		}
	}
	for name, want := range map[string]string{
		"One": "1", "Two": "2", "Three": "3", "Four": "4", "Five": "5", "Six": "6",
		"Setext Two": "2",
	} {
		if levels[name] != want {
			t.Errorf("heading %q level = %q, want %q", name, levels[name], want)
		}
	}
}
