package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
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

	byType := map[string][]string{}
	for _, e := range doc.Entities {
		byType[e.Type] = append(byType[e.Type], e.Name)
	}

	contains := func(slice []string, s string) bool {
		for _, v := range slice {
			if v == s {
				return true
			}
		}
		return false
	}

	if !contains(byType["import"], "b") {
		t.Errorf("expected import b, got %v", byType)
	}
	if !contains(byType["interface"], "I") {
		t.Errorf("expected interface I, got %v", byType)
	}
	if !contains(byType["type-alias"], "T") {
		t.Errorf("expected type-alias T, got %v", byType)
	}
	if !contains(byType["enum"], "E") {
		t.Errorf("expected enum E, got %v", byType)
	}
	if !contains(byType["function"], "f") {
		t.Errorf("expected function f, got %v", byType)
	}
	if !contains(byType["export"], "f") {
		t.Errorf("expected export f, got %v", byType)
	}

	if len(doc.Entities) != 6 {
		t.Errorf("expected 6 entities, got %d: %v", len(doc.Entities), byType)
	}

	// `export function f()` resolves to an "exports" link carrying the
	// extracted provenance tag.
	if len(doc.Links) != 1 {
		t.Fatalf("expected 1 exports link, got %d: %v", len(doc.Links), doc.Links)
	}
	if doc.Links[0].Type != "exports" {
		t.Errorf("link Type = %q, want exports", doc.Links[0].Type)
	}
	if doc.Links[0].SourceType != ir.LinkSourceExtracted {
		t.Errorf("link SourceType = %q, want %q", doc.Links[0].SourceType, ir.LinkSourceExtracted)
	}
}

func TestParse_TSFile_NestedClasses(t *testing.T) {
	tmp := t.TempDir()
	src := `export class Outer {
  innerMethod() {}
}
class Second {
  static create() { return new Second() }
}
`
	path := filepath.Join(tmp, "nested.ts")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "typescript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byType := map[string][]string{}
	for _, e := range doc.Entities {
		byType[e.Type] = append(byType[e.Type], e.Name)
	}

	contains := func(slice []string, s string) bool {
		for _, v := range slice {
			if v == s {
				return true
			}
		}
		return false
	}

	if !contains(byType["class"], "Outer") {
		t.Errorf("expected class Outer, got %v", byType)
	}
	if !contains(byType["class"], "Second") {
		t.Errorf("expected class Second, got %v", byType)
	}
	if !contains(byType["method"], "Outer.innerMethod") {
		t.Errorf("expected method Outer.innerMethod, got %v", byType)
	}
	if !contains(byType["method"], "Second.create") {
		t.Errorf("expected method Second.create, got %v", byType)
	}
	if !contains(byType["export"], "Outer") {
		t.Errorf("expected export Outer, got %v", byType)
	}
}

func TestParse_TSFile_EnumWithValues(t *testing.T) {
	tmp := t.TempDir()
	src := `enum Status {
  Active = "ACTIVE",
  Inactive = "INACTIVE",
  Pending = 0,
}
`
	path := filepath.Join(tmp, "status.ts")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "typescript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	found := false
	for _, e := range doc.Entities {
		if e.Name == "Status" && e.Type == "enum" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected enum entity Status, got %v", doc.Entities)
	}
}

func TestParse_TSFile_InterfaceMethods(t *testing.T) {
	tmp := t.TempDir()
	src := `interface Repository {
  findById(id: string): Promise<Entity>
  save(entity: Entity): void
  delete(id: string): boolean
}
`
	path := filepath.Join(tmp, "repo.ts")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "typescript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["Repository"] != "interface" {
		t.Errorf("Repository type = %q, want interface", byName["Repository"])
	}
}

func TestParse_TSFile_TypeAlias(t *testing.T) {
	tmp := t.TempDir()
	src := `type StringOrNumber = string | number
type Callback = (data: string) => void
type Nullable<T> = T | null
`
	path := filepath.Join(tmp, "types.ts")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "typescript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["StringOrNumber"] != "type-alias" {
		t.Errorf("StringOrNumber type = %q, want type-alias", byName["StringOrNumber"])
	}
	if byName["Callback"] != "type-alias" {
		t.Errorf("Callback type = %q, want type-alias", byName["Callback"])
	}
	if byName["Nullable"] != "type-alias" {
		t.Errorf("Nullable type = %q, want type-alias", byName["Nullable"])
	}
	if len(doc.Entities) != 3 {
		t.Errorf("expected 3 entities, got %d: %v", len(doc.Entities), byName)
	}
}

func TestParse_TSXFile_ComponentPatterns(t *testing.T) {
	tmp := t.TempDir()
	src := `import React from 'react';

interface ButtonProps {
  label: string
  onClick: () => void
}

const Button: React.FC<ButtonProps> = ({ label, onClick }) => {
  return <button onClick={onClick}>{label}</button>
}

export default Button
`
	path := filepath.Join(tmp, "Button.tsx")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "tsx", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byType := map[string][]string{}
	for _, e := range doc.Entities {
		byType[e.Type] = append(byType[e.Type], e.Name)
	}

	contains := func(slice []string, s string) bool {
		for _, v := range slice {
			if v == s {
				return true
			}
		}
		return false
	}

	if !contains(byType["import"], "react") {
		t.Errorf("expected import react, got %v", byType)
	}
	if !contains(byType["interface"], "ButtonProps") {
		t.Errorf("expected interface ButtonProps, got %v", byType)
	}
	if !contains(byType["function"], "Button") {
		t.Errorf("expected function Button, got %v", byType)
	}
	if !contains(byType["export"], "Button") {
		t.Errorf("expected export Button, got %v", byType)
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

	byType := map[string][]string{}
	for _, e := range doc.Entities {
		byType[e.Type] = append(byType[e.Type], e.Name)
	}

	contains := func(slice []string, s string) bool {
		for _, v := range slice {
			if v == s {
				return true
			}
		}
		return false
	}

	if !contains(byType["import"], "react") {
		t.Errorf("expected import react, got %v", byType)
	}
	if !contains(byType["interface"], "Props") {
		t.Errorf("expected interface Props, got %v", byType)
	}
	if !contains(byType["function"], "App") {
		t.Errorf("expected function App, got %v", byType)
	}
	if !contains(byType["export"], "App") {
		t.Errorf("expected export App, got %v", byType)
	}
}
