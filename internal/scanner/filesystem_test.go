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

func TestFileSystemScanner_Scan_DefaultSkip(t *testing.T) {
	tmp := t.TempDir()

	files := map[string]string{
		"src/index.js":          "import App from './App'",
		"src/App.jsx":           "export default () => null",
		"README.md":             "# Notes",
		"docs/manual.pdf":       "pdf",
		"data/report.xlsx":      "xlsx",
		".git/config":           "hidden",
		".git/objects/abc123":   "hidden",
		".hg/store/foo":         "hidden",
		"assets/logo.png":       "png",
		"assets/font.woff2":     "font",
		"static/hero.jpg":       "jpg",
		"static/hero.svg":       "svg",
		"static/app.min.js.map": "map",
		"archives/src.tar.gz":   "tar",
		"package-lock.json":     "{}",
		"yarn.lock":             "yarn",
		"go.sum":                "checksum",
		"native/binary.wasm":    "wasm",
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

	byPath := make(map[string]File)
	for _, f := range got {
		rel, _ := filepath.Rel(tmp, f.Path)
		byPath[rel] = f
	}

	// Source-adjacent formats with dedicated extractors stay indexable.
	for _, keep := range []string{"src/index.js", "src/App.jsx", "README.md", "docs/manual.pdf", "data/report.xlsx"} {
		if _, ok := byPath[keep]; !ok {
			t.Errorf("default skip dropped %q, want it kept", keep)
		}
	}

	// VCS internals, binary assets, source maps and lockfiles are noise.
	for _, drop := range []string{
		".git/config", ".git/objects/abc123", ".hg/store/foo",
		"assets/logo.png", "assets/font.woff2", "static/hero.jpg",
		"static/hero.svg", "static/app.min.js.map", "archives/src.tar.gz",
		"package-lock.json", "yarn.lock", "go.sum", "native/binary.wasm",
	} {
		if _, ok := byPath[drop]; ok {
			t.Errorf("default skip kept %q, want it dropped", drop)
		}
	}
}

func TestFileSystemScanner_Scan_NoDefaultSkip(t *testing.T) {
	tmp := t.TempDir()
	for _, p := range []string{".git/config", "assets/logo.png", "package-lock.json", "src/index.js"} {
		full := filepath.Join(tmp, p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	s := FileSystemScanner{NoDefaultSkip: true}
	got, err := s.Scan(tmp)
	if err != nil {
		t.Fatalf("Scan returned unexpected error: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("NoDefaultSkip Scan returned %d files, want 4", len(got))
	}
}

func TestFileSystemScanner_Scan_PopulatesUpdatedAt(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "main.go")
	if err := os.WriteFile(path, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	var s FileSystemScanner
	got, err := s.Scan(tmp)
	if err != nil {
		t.Fatalf("Scan returned unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Scan returned %d files, want 1", len(got))
	}
	if got[0].UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be populated from the file's modification time")
	}
	want, _ := os.Stat(path)
	if !got[0].UpdatedAt.Equal(want.ModTime()) {
		t.Errorf("UpdatedAt = %v, want %v", got[0].UpdatedAt, want.ModTime())
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
