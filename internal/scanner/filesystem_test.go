package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "go file",
			path:     "main.go",
			expected: "golang",
		},
		{
			name:     "go file in subdirectory",
			path:     "internal/parser/parser.go",
			expected: "golang",
		},
		{
			name:     "pdf file",
			path:     "docs/architecture.pdf",
			expected: "pdf",
		},
		{
			name:     "markdown extension",
			path:     "README.md",
			expected: "markdown",
		},
		{
			name:     "markdown full extension",
			path:     "docs/guide.markdown",
			expected: "markdown",
		},
		{
			name:     "xlsx spreadsheet",
			path:     "data/report.xlsx",
			expected: "spreadsheet",
		},
		{
			name:     "xls spreadsheet",
			path:     "data/legacy.xls",
			expected: "spreadsheet",
		},
		{
			name:     "csv spreadsheet",
			path:     "data/export.csv",
			expected: "spreadsheet",
		},
		{
			name:     "unknown extension falls back to unstructured",
			path:     "file.xyz",
			expected: "unstructured",
		},
		{
			name:     "makefile by filename",
			path:     "Makefile",
			expected: "make",
		},
		{
			name:     "gnu makefile by filename",
			path:     "GNUmakefile",
			expected: "make",
		},
		{
			name:     "makefile by extension",
			path:     "build.mk",
			expected: "make",
		},
		{
			name:     "dockerfile by filename",
			path:     "Dockerfile",
			expected: "dockerfile",
		},
		{
			name:     "containerfile by filename",
			path:     "Containerfile",
			expected: "dockerfile",
		},
		{
			name:     "dockerfile by extension",
			path:     "docker/app.dockerfile",
			expected: "dockerfile",
		},
		{
			name:     "json file",
			path:     "package.json",
			expected: "json",
		},
		{
			name:     "yaml file",
			path:     "config.yaml",
			expected: "yaml",
		},
		{
			name:     "yml file",
			path:     "config.yml",
			expected: "yaml",
		},
		{
			name:     "toml file",
			path:     "config.toml",
			expected: "toml",
		},
		{
			name:     "javascript file",
			path:     "src/app.js",
			expected: "javascript",
		},
		{
			name:     "jsx file",
			path:     "src/App.jsx",
			expected: "javascript",
		},
		{
			name:     "es module file",
			path:     "src/util.mjs",
			expected: "javascript",
		},
		{
			name:     "typescript file",
			path:     "src/app.ts",
			expected: "typescript",
		},
		{
			name:     "tsx file",
			path:     "src/App.tsx",
			expected: "tsx",
		},
		{
			name:     "uppercase extension is normalised",
			path:     "File.GO",
			expected: "golang",
		},
		{
			name:     "uppercase pdf",
			path:     "scan.PDF",
			expected: "pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectLanguage(tt.path)
			if got != tt.expected {
				t.Errorf("detectLanguage(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestFileSystemScanner_Scan(t *testing.T) {
	// Create a temporary directory tree with known files.
	tmp := t.TempDir()

	files := map[string]string{
		"main.go":         "package main",
		"util/helper.go":  "package util",
		"docs/notes.md":   "# Notes",
		"data/report.csv": "a,b,c",
		"README":          "hello",
	}

	for path, content := range files {
		full := filepath.Join(tmp, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var s FileSystemScanner
	got, err := s.Scan(tmp)
	if err != nil {
		t.Fatalf("Scan returned unexpected error: %v", err)
	}

	if len(got) != len(files) {
		t.Fatalf("Scan returned %d files, want %d", len(got), len(files))
	}

	// Build a lookup map so we can assert individual entries regardless of walk order.
	byPath := make(map[string]File)
	for _, f := range got {
		// Paths from WalkDir are absolute; convert to relative for comparison.
		rel, _ := filepath.Rel(tmp, f.Path)
		byPath[rel] = f
	}

	tests := []struct {
		path    string
		lang    string
		minSize int64
	}{
		{"main.go", "golang", 12},
		{"util/helper.go", "golang", 12},
		{"docs/notes.md", "markdown", 7},
		{"data/report.csv", "spreadsheet", 5},
		{"README", "unstructured", 5},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			f, ok := byPath[tt.path]
			if !ok {
				t.Fatalf("file %q not found in scan results", tt.path)
			}
			if f.Language != tt.lang {
				t.Errorf("Language = %q, want %q", f.Language, tt.lang)
			}
			if f.Size < tt.minSize {
				t.Errorf("Size = %d, want >= %d", f.Size, tt.minSize)
			}
		})
	}
}

func TestFileSystemScanner_Scan_NonexistentRoot(t *testing.T) {
	var s FileSystemScanner
	_, err := s.Scan("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("Scan on nonexistent root should return an error")
	}
}

func TestFileSystemScanner_Scan_EmptyDirectory(t *testing.T) {
	tmp := t.TempDir()
	var s FileSystemScanner
	got, err := s.Scan(tmp)
	if err != nil {
		t.Fatalf("Scan on empty directory returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Scan on empty directory returned %d files, want 0", len(got))
	}
}
