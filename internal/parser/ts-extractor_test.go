package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestParse_TSFile(t *testing.T) {
	tmp := t.TempDir()
	src := `import { A } from 'b';
interface I { a: string }
type T = string;
enum E { X }
export function f() {}
`
	path := filepath.Join(tmp, "app.ts")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "typescript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Metadata["processor"] != "ts-extractor" {
		t.Errorf("Metadata[processor] = %q, want ts-extractor", doc.Metadata["processor"])
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["b"] != "import" {
		t.Errorf("import b type = %q, want import", byName["b"])
	}
	if byName["I"] != "interface" {
		t.Errorf("I type = %q, want interface", byName["I"])
	}
	if byName["T"] != "type-alias" {
		t.Errorf("T type = %q, want type-alias", byName["T"])
	}
	if byName["E"] != "enum" {
		t.Errorf("E type = %q, want enum", byName["E"])
	}
	if byName["f"] != "function" {
		t.Errorf("f type = %q, want function", byName["f"])
	}

	if len(doc.Entities) != 5 {
		t.Errorf("expected 5 entities, got %d: %v", len(doc.Entities), byName)
	}
}

func TestParse_TSXFile(t *testing.T) {
	tmp := t.TempDir()
	src := `import React from 'react';
interface Props { name: string }
export const App: React.FC<Props> = () => <div>hi</div>;
`
	path := filepath.Join(tmp, "App.tsx")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "tsx", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Metadata["processor"] != "ts-extractor" {
		t.Errorf("Metadata[processor] = %q, want ts-extractor", doc.Metadata["processor"])
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["react"] != "import" {
		t.Errorf("import react type = %q, want import", byName["react"])
	}
	if byName["Props"] != "interface" {
		t.Errorf("Props type = %q, want interface", byName["Props"])
	}
	if byName["App"] != "function" {
		t.Errorf("App type = %q, want function", byName["App"])
	}
}
