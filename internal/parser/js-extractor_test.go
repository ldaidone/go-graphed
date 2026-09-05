package parser

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/ir"
	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestParse_JSFile_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.js")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "javascript", Size: 0}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty JS file, got %d", len(doc.Entities))
	}
}

func TestParse_JSFile_MultipleArrowFunctions(t *testing.T) {
	tmp := t.TempDir()
	src := `const add = (a, b) => a + b;
const mul = (a, b) => a * b;
const noop = () => {};
`
	path := filepath.Join(tmp, "arrows.js")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "javascript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["add"] != "function" {
		t.Errorf("add type = %q, want function", byName["add"])
	}
	if byName["mul"] != "function" {
		t.Errorf("mul type = %q, want function", byName["mul"])
	}
	if byName["noop"] != "function" {
		t.Errorf("noop type = %q, want function", byName["noop"])
	}
	if len(doc.Entities) != 3 {
		t.Errorf("expected 3 entities, got %d: %v", len(doc.Entities), byName)
	}
}

func TestParse_JSFile_ExportDefaultFunction(t *testing.T) {
	tmp := t.TempDir()
	src := `export default function hello() { return 'hi' }
export function world() { return 'world' }
`
	path := filepath.Join(tmp, "exports.js")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "javascript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byType := map[string][]string{}
	for _, e := range doc.Entities {
		byType[e.Type] = append(byType[e.Type], e.Name)
	}

	// Functions should be extracted.
	contains := func(slice []string, s string) bool {
		return slices.Contains(slice, s)
	}
	if !contains(byType["function"], "hello") {
		t.Errorf("expected function hello, got %v", byType)
	}
	if !contains(byType["function"], "world") {
		t.Errorf("expected function world, got %v", byType)
	}
	// Export entities should also exist.
	if !contains(byType["export"], "hello") {
		t.Errorf("expected export hello, got %v", byType)
	}
	if !contains(byType["export"], "world") {
		t.Errorf("expected export world, got %v", byType)
	}
}

func TestParse_JSFile_NestedClasses(t *testing.T) {
	tmp := t.TempDir()
	src := `class Outer {
  constructor() {}
  outerMethod() {}
  static factory() { return new Outer() }
}
class Inner {
  innerMethod() {}
}
`
	path := filepath.Join(tmp, "classes.js")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "javascript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["Outer"] != "class" {
		t.Errorf("Outer type = %q, want class", byName["Outer"])
	}
	if byName["Inner"] != "class" {
		t.Errorf("Inner type = %q, want class", byName["Inner"])
	}
	if byName["Outer.outerMethod"] != "method" {
		t.Errorf("Outer.outerMethod type = %q, want method", byName["Outer.outerMethod"])
	}
	if byName["Outer.factory"] != "method" {
		t.Errorf("Outer.factory type = %q, want method", byName["Outer.factory"])
	}
	if byName["Inner.innerMethod"] != "method" {
		t.Errorf("Inner.innerMethod type = %q, want method", byName["Inner.innerMethod"])
	}
}

func TestParse_JSFile_MixedImports(t *testing.T) {
	tmp := t.TempDir()
	src := `import React from 'react';
import { useState, useEffect } from 'react';
import fs from 'node:fs';
import './styles.css';
`
	path := filepath.Join(tmp, "imports.js")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "javascript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}

	if byName["react"] != "import" {
		t.Errorf("react import type = %q, want import", byName["react"])
	}
	if byName["node:fs"] != "import" {
		t.Errorf("node:fs import type = %q, want import", byName["node:fs"])
	}
	if byName["./styles.css"] != "import" {
		t.Errorf("./styles.css import type = %q, want import", byName["./styles.css"])
	}
}

func TestParse_JSFile_SyntacticallyInvalid(t *testing.T) {
	tmp := t.TempDir()
	src := `function f() {
  this is not valid javascript
  {{{
`
	path := filepath.Join(tmp, "invalid.js")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "javascript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	// Tree-sitter may or may not recover the function declaration.
	// The important invariant is that no entity has empty ID or Name.
	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}

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

	byType := map[string][]string{}
	for _, e := range doc.Entities {
		byType[e.Type] = append(byType[e.Type], e.Name)
	}

	contains := func(slice []string, s string) bool {
		return slices.Contains(slice, s)
	}

	if !contains(byType["import"], "y") {
		t.Errorf("expected import y, got %v", byType)
	}
	if !contains(byType["function"], "foo") {
		t.Errorf("expected function foo, got %v", byType)
	}
	if !contains(byType["function"], "baz") {
		t.Errorf("expected function baz (arrow), got %v", byType)
	}
	if !contains(byType["class"], "A") {
		t.Errorf("expected class A, got %v", byType)
	}
	if !contains(byType["method"], "A.m") {
		t.Errorf("expected method A.m, got %v", byType)
	}
	if !contains(byType["export"], "c") {
		t.Errorf("expected export c, got %v", byType)
	}
	if !contains(byType["variable"], "c") {
		t.Errorf("expected variable c, got %v", byType)
	}

	if len(doc.Entities) != 7 {
		t.Errorf("expected 7 entities, got %d: %v", len(doc.Entities), byType)
	}
}

func TestParse_JSFile_CallLinks(t *testing.T) {
	tmp := t.TempDir()
	src := `function helper() { return 42; }
function main() { return helper(); }
`
	path := filepath.Join(tmp, "calls.js")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "javascript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if len(doc.Links) != 1 {
		t.Fatalf("expected 1 call link, got %d: %v", len(doc.Links), doc.Links)
	}

	link := doc.Links[0]
	if link.Type != "calls" {
		t.Errorf("link type = %q, want calls", link.Type)
	}
	if !strings.Contains(link.SourceID, "main") {
		t.Errorf("link source should contain main, got %q", link.SourceID)
	}
	if !strings.Contains(link.TargetID, "helper") {
		t.Errorf("link target should contain helper, got %q", link.TargetID)
	}
	if link.SourceType != ir.LinkSourceExtracted {
		t.Errorf("link SourceType = %q, want %q", link.SourceType, ir.LinkSourceExtracted)
	}
}

func TestParse_JSFile_VariableDeclarations(t *testing.T) {
	tmp := t.TempDir()
	src := `const x = 42;
let y = "hello";
var z = [1, 2, 3];
`
	path := filepath.Join(tmp, "vars.js")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "javascript", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	byType := map[string][]string{}
	for _, e := range doc.Entities {
		byType[e.Type] = append(byType[e.Type], e.Name)
	}

	contains := func(slice []string, s string) bool {
		return slices.Contains(slice, s)
	}

	if !contains(byType["variable"], "x") {
		t.Errorf("expected variable x, got %v", byType)
	}
	if !contains(byType["variable"], "y") {
		t.Errorf("expected variable y, got %v", byType)
	}
	if !contains(byType["variable"], "z") {
		t.Errorf("expected variable z, got %v", byType)
	}
}
