package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func scanNames(t *testing.T, s *FileSystemScanner, root string) map[string]bool {
	t.Helper()
	got, err := s.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned unexpected error: %v", err)
	}
	names := map[string]bool{}
	for _, f := range got {
		rel, _ := filepath.Rel(root, f.Path)
		names[rel] = true
	}
	return names
}

func assertPresent(t *testing.T, names map[string]bool, path string) {
	t.Helper()
	if !names[path] {
		t.Errorf("expected %q to be scanned", path)
	}
}

func assertAbsent(t *testing.T, names map[string]bool, path string) {
	t.Helper()
	if names[path] {
		t.Errorf("expected %q to be excluded", path)
	}
}

func TestScan_HonorsGitIgnore(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		".gitignore":    "*.log\nbuild/\n!keep.log\n",
		"app.go":        "package main",
		"trace.log":     "trace",
		"keep.log":      "keep",
		"build/out.bin": "bin",
		"notes.md":      "# Notes",
	})

	names := scanNames(t, &FileSystemScanner{}, tmp)

	assertPresent(t, names, "app.go")
	assertPresent(t, names, "notes.md")
	// *.log excluded, but the negation re-includes keep.log.
	assertAbsent(t, names, "trace.log")
	assertPresent(t, names, "keep.log")
	// build/ dir-only pattern prunes the whole tree.
	assertAbsent(t, names, "build/out.bin")
	// .gitignore itself is configuration, not content.
	assertAbsent(t, names, ".gitignore")
}

func TestScan_NestedGitIgnoreOverridesRoot(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		".gitignore":     "*.log\n",
		"root.log":       "root",
		"sub/.gitignore": "*.log\n!keep.log\n",
		"sub/keep.log":   "keep",
		"sub/drop.log":   "drop",
	})

	names := scanNames(t, &FileSystemScanner{}, tmp)

	assertAbsent(t, names, "root.log")
	// Deepest-first ordering: sub/ re-includes keep.log but not drop.log.
	// Note: the re-inclusion only works because the nested file also contains
	// a positive pattern; a nested file holding a lone "!keep.log" cannot be
	// detected as a match by the underlying matcher (documented limitation).
	assertPresent(t, names, "sub/keep.log")
	assertAbsent(t, names, "sub/drop.log")
}

func TestScan_ExcludePatternsLayerOnTop(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		".gitignore": "keep.log\n",
		"keep.log":   "keep",
		"gen.go":     "package main",
	})

	// A user --exclude wins even though .gitignore would re-include keep.log.
	names := scanNames(t, &FileSystemScanner{Exclude: []string{"!keep.log"}}, tmp)
	assertAbsent(t, names, "keep.log")
	assertPresent(t, names, "gen.go")
}

func TestScan_CustomIgnoreFile(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		".graphed.ignore": "*.tmp\n",
		"data.csv":        "a,b,c",
		"scratch.tmp":     "tmp",
	})

	names := scanNames(t, &FileSystemScanner{IgnoreFiles: []string{".graphed.ignore"}}, tmp)

	assertAbsent(t, names, "scratch.tmp")
	assertPresent(t, names, "data.csv")
}

func TestScan_NoGitIgnoreDisablesDiscovery(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		".gitignore": "*.log\n",
		"trace.log":  "trace",
		"notes.md":   "# Notes",
	})

	names := scanNames(t, &FileSystemScanner{NoGitIgnore: true}, tmp)

	// Without gitignore handling, trace.log is scanned and .gitignore is skipped
	// only because of the hard-coded name filter.
	assertPresent(t, names, "trace.log")
	assertPresent(t, names, "notes.md")
}

func TestScan_MalformedGitIgnoreDoesNotAbort(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		// An unterminated bracket would make a strict parser fail.
		".gitignore": "[unterminated\n",
		"app.go":     "package main",
	})

	names := scanNames(t, &FileSystemScanner{}, tmp)
	assertPresent(t, names, "app.go")
}
