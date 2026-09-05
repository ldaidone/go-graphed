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

func parseBash(t *testing.T, name, src string) ir.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	file := scanner.File{Path: path, Language: "bash", Size: int64(len(src))}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	return doc
}

func TestParse_BashFile_EmptyFile(t *testing.T) {
	doc := parseBash(t, "empty.sh", "")
	if len(doc.Entities) != 0 {
		t.Errorf("expected 0 entities for empty Bash file, got %d", len(doc.Entities))
	}
	if doc.Metadata["processor"] != "bash-extractor" {
		t.Errorf("processor = %q, want bash-extractor", doc.Metadata["processor"])
	}
}

func TestParse_BashFile_Functions(t *testing.T) {
	src := `#!/usr/bin/env bash
set -euo pipefail

log() {
    echo "[log]" "$@"
}

run_build() {
    log "building"
    make build
}

main() {
    run_build
}
`
	doc := parseBash(t, "script.sh", src)

	byName := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
	}
	for _, want := range []string{"log", "run_build", "main"} {
		if byName[want] != "function" {
			t.Errorf("%s type = %q, want function", want, byName[want])
		}
	}
	if byName["GREETING"] != "" {
		t.Errorf("variable assignments must not be extracted, got %q", byName["GREETING"])
	}
}

func TestParse_BashFile_Calls(t *testing.T) {
	src := `log() {
    echo "$@"
}

run_build() {
    log "building"
    make build
}

helper() {
    echo "helper"
}

main() {
    run_build app
    helper
}
`
	doc := parseBash(t, "calls.sh", src)

	names := nameByID(doc)
	calls := map[string][]string{}
	for _, l := range doc.Links {
		if l.Type != "calls" {
			t.Errorf("unexpected link type %q: %+v", l.Type, l)
			continue
		}
		if l.SourceType != ir.LinkSourceExtracted {
			t.Errorf("call provenance = %q, want %q", l.SourceType, ir.LinkSourceExtracted)
		}
		calls[names[l.SourceID]] = append(calls[names[l.SourceID]], names[l.TargetID])
	}

	contains := func(slice []string, s string) bool {
		return slices.Contains(slice, s)
	}

	if !contains(calls["run_build"], "log") {
		t.Errorf("run_build should call log, got %v", calls["run_build"])
	}
	if !contains(calls["main"], "run_build") {
		t.Errorf("main should call run_build, got %v", calls["main"])
	}
	if !contains(calls["main"], "helper") {
		t.Errorf("main should call helper, got %v", calls["main"])
	}

	// External commands must never resolve, and top-level calls have no
	// enclosing function.
	for _, l := range doc.Links {
		if strings.Contains(l.TargetID, "make") || strings.Contains(l.TargetID, "echo") {
			t.Errorf("external command resolved as a call: %+v", l)
		}
	}
}

func TestParse_BashFile_SyntacticallyInvalid(t *testing.T) {
	src := `function broken( {
    echo "oops"
}
`
	doc := parseBash(t, "invalid.sh", src)
	for _, e := range doc.Entities {
		if e.ID == "" || e.Name == "" {
			t.Errorf("entity has empty ID or Name: %+v", e)
		}
	}
}
