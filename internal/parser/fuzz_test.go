package parser

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

// fuzzExtractorBody is the shared property check for every text extractor:
//   - the extractor must not panic on arbitrary bytes,
//   - a successful run must be deterministic across repeated runs,
//   - every produced entity must carry a non-empty ID and Name.
//
// Parse errors are acceptable (most inputs are garbage); only panics,
// nondeterminism, and malformed entities fail the fuzz target.
func fuzzExtractorBody(t *testing.T, extract func(string) ([]ir.Entity, error), data []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	first, err := extract(path)
	if err != nil {
		return
	}

	second, err := extract(path)
	if err != nil {
		t.Fatalf("second run errored after first succeeded: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("extractor output is nondeterministic")
	}

	for _, e := range first {
		if e.ID == "" || e.Name == "" {
			t.Fatalf("entity with empty ID or Name: %+v", e)
		}
	}
}

func FuzzExtractJSON(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"a": {"b": [1, 2]}, "c": "d"}`),
		[]byte(``),
		[]byte(`{`),
		[]byte(`{"x": "y"}`),
		[]byte(`[1, 2, {"k": "v"}]`),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzExtractorBody(t, extractJSONData, data)
	})
}

func FuzzExtractYAML(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("a: 1\nb:\n  - x\n  - y\n"),
		[]byte(``),
		[]byte(`a: {b: 1}`),
		[]byte("key: value\n"),
		[]byte("- one\n- two\n"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzExtractorBody(t, extractYAMLData, data)
	})
}

func FuzzExtractTOML(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("[server]\nhost = \"localhost\"\nport = 8080\n"),
		[]byte(``),
		[]byte("[a.b]\nx = 1\n"),
		[]byte("key = \"value\"\n"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzExtractorBody(t, extractTOMLData, data)
	})
}

func FuzzExtractGo(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("package main\n\ntype T struct{}\n\nfunc (t *T) M() {}\n\nfunc main() { help() }\n\nfunc help() {}\n"),
		[]byte(``),
		[]byte("package main\n"),
		[]byte("package main\nfunc a() { b() }\nfunc b() {}\n"),
		[]byte(`package main
import (
	"fmt"
)
func main() { fmt.Println("hi") }
`),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "input.go")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}

		first, links1, err := extractGoData(path)
		if err != nil {
			return
		}
		second, links2, err := extractGoData(path)
		if err != nil {
			t.Fatalf("second run errored after first succeeded: %v", err)
		}
		if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(links1, links2) {
			t.Fatal("go extractor output is nondeterministic")
		}
		for _, e := range first {
			if e.ID == "" || e.Name == "" {
				t.Fatalf("entity with empty ID or Name: %+v", e)
			}
		}
		for _, l := range links1 {
			if l.SourceID == "" || l.TargetID == "" {
				t.Fatalf("link with empty endpoint: %+v", l)
			}
		}
	})
}

func FuzzExtractJS(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("import x from 'y';\nclass A { m() {} }\nfunction f() {}\nconst g = () => {};\n"),
		[]byte(``),
		[]byte("function f() {}\n"),
		[]byte("const x = () => 1;\n"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "input.js")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}

		first, links1, err := extractJSData(path)
		if err != nil {
			return
		}
		second, links2, err := extractJSData(path)
		if err != nil {
			t.Fatalf("second run errored after first succeeded: %v", err)
		}
		if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(links1, links2) {
			t.Fatal("js extractor output is nondeterministic")
		}
		for _, e := range first {
			if e.ID == "" || e.Name == "" {
				t.Fatalf("entity with empty ID or Name: %+v", e)
			}
		}
		for _, l := range links1 {
			if l.SourceID == "" || l.TargetID == "" {
				t.Fatalf("link with empty endpoint: %+v", l)
			}
		}
	})
}

func FuzzExtractTS(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("import { A } from 'b';\ninterface I { a: string }\ntype T = string;\nenum E { X }\n"),
		[]byte(``),
		[]byte("interface Props {\n  name: string\n}\n"),
		[]byte("type ID = string | number;\n"),
		[]byte("enum Color { Red, Green, Blue }\n"),
		[]byte("export function f(): void {}\n"),
		[]byte("export class Box<T> {\n  private value: T\n  constructor(v: T) { this.value = v }\n}\n"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "input.ts")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}

		first, links1, err := extractTSData(path)
		if err != nil {
			return
		}
		second, links2, err := extractTSData(path)
		if err != nil {
			t.Fatalf("second run errored after first succeeded: %v", err)
		}
		if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(links1, links2) {
			t.Fatal("ts extractor output is nondeterministic")
		}
		for _, e := range first {
			if e.ID == "" || e.Name == "" {
				t.Fatalf("entity with empty ID or Name: %+v", e)
			}
		}
		for _, l := range links1 {
			if l.SourceID == "" || l.TargetID == "" {
				t.Fatalf("link with empty endpoint: %+v", l)
			}
		}
	})
}

func FuzzExtractTSX(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("import React from 'react';\nexport const App = () => <div>hi</div>;\n"),
		[]byte(``),
		[]byte("interface Props { name: string }\nconst Greeting: React.FC<Props> = (p) => <span>{p.name}</span>;\n"),
		[]byte("export default function Page() { return <main>ok</main> }\n"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "input.tsx")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}

		first, links1, err := extractTSXData(path)
		if err != nil {
			return
		}
		second, links2, err := extractTSXData(path)
		if err != nil {
			t.Fatalf("second run errored after first succeeded: %v", err)
		}
		if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(links1, links2) {
			t.Fatal("tsx extractor output is nondeterministic")
		}
		for _, e := range first {
			if e.ID == "" || e.Name == "" {
				t.Fatalf("entity with empty ID or Name: %+v", e)
			}
		}
		for _, l := range links1 {
			if l.SourceID == "" || l.TargetID == "" {
				t.Fatalf("link with empty endpoint: %+v", l)
			}
		}
	})
}

func FuzzExtractMarkdown(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("# Title\n\n## Section\n\nSee [link](https://example.com).\n"),
		[]byte(``),
		[]byte("plain text\n"),
		[]byte("# a\n# a\n"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzExtractorBody(t, extractMarkdownData, data)
	})
}

func FuzzExtractDockerfile(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("FROM golang:1.22\nRUN go build .\nCOPY . .\n"),
		[]byte(``),
		[]byte("FROM scratch\n"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzExtractorBody(t, extractDockerfileData, data)
	})
}

func FuzzExtractMakefile(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("all: build\n\tgo build\n\nbuild:\n\tgo build .\n\nCC = gcc\n"),
		[]byte(``),
		[]byte("VAR = value\n"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzExtractorBody(t, extractMakefileData, data)
	})
}

func FuzzParse(f *testing.F) {
	for _, lang := range []string{
		"golang", "json", "yaml", "toml", "javascript",
		"typescript", "tsx", "dockerfile", "make",
		"markdown", "pdf", "spreadsheet", "unknown-language",
	} {
		f.Add(lang, []byte(""))
	}
	f.Add("golang", []byte("package main\nfunc main() {}\n"))
	f.Add("json", []byte(`{"a": [1, {"b": 2}]}`))
	f.Add("markdown", []byte("# hi\n"))
	f.Add("typescript", []byte("interface X {}\n"))

	f.Fuzz(func(t *testing.T, lang string, data []byte) {
		path := filepath.Join(t.TempDir(), "input")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		doc, err := Parse(scanner.File{Path: path, Language: lang})
		if err != nil {
			return
		}
		if doc.Format != lang {
			t.Fatalf("Parse().Format = %q, want %q", doc.Format, lang)
		}
	})
}
