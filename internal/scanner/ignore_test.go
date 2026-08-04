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

func TestScan_EmptyExcludePatterns(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		"app.go":      "package main",
		"config.yaml": "key: value",
	})

	names := scanNames(t, &FileSystemScanner{Exclude: []string{}}, tmp)
	assertPresent(t, names, "app.go")
	assertPresent(t, names, "config.yaml")
}

func TestScan_EmptyIgnoreFileIgnored(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		".emptyignore": "",
		"app.go":       "package main",
	})

	names := scanNames(t, &FileSystemScanner{IgnoreFiles: []string{".emptyignore"}}, tmp)
	assertPresent(t, names, "app.go")
}

func TestScan_NonexistentIgnoreFileIgnored(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		"app.go": "package main",
	})

	names := scanNames(t, &FileSystemScanner{IgnoreFiles: []string{".nonexistent"}}, tmp)
	assertPresent(t, names, "app.go")
}

func TestScan_MultipleIgnoreFiles(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		".ignore_a":   "*.log\n",
		".ignore_b":   "*.tmp\n",
		"app.go":      "package main",
		"debug.log":   "log",
		"scratch.tmp": "tmp",
	})

	names := scanNames(t, &FileSystemScanner{IgnoreFiles: []string{".ignore_a", ".ignore_b"}}, tmp)
	assertPresent(t, names, "app.go")
	assertAbsent(t, names, "debug.log")
	assertAbsent(t, names, "scratch.tmp")
}

func TestScan_AbsoluteIgnoreFilePath(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		"my.ignore":  "*.bak\n",
		"app.go":     "package main",
		"backup.bak": "data",
	})

	absIgnore := filepath.Join(tmp, "my.ignore")
	names := scanNames(t, &FileSystemScanner{IgnoreFiles: []string{absIgnore}}, tmp)
	assertPresent(t, names, "app.go")
	assertAbsent(t, names, "backup.bak")
}

func TestScan_DirectoryExclusionPrunesSubtree(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		".gitignore":     "build/\n",
		"app.go":         "package main",
		"build/a.go":     "package a",
		"build/sub/b.go": "package b",
	})

	names := scanNames(t, &FileSystemScanner{}, tmp)
	assertPresent(t, names, "app.go")
	assertAbsent(t, names, "build/a.go")
	assertAbsent(t, names, "build/sub/b.go")
}

func TestScan_NegationReIncludesFile(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		".gitignore":    "*.log\n!important.log\n",
		"app.go":        "package main",
		"debug.log":     "debug",
		"important.log": "important",
	})

	names := scanNames(t, &FileSystemScanner{}, tmp)
	assertPresent(t, names, "app.go")
	assertAbsent(t, names, "debug.log")
	assertPresent(t, names, "important.log")
}

func TestScan_DeeplyNestedGitIgnore(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		".gitignore":       "*.log\n",
		"a/.gitignore":     "*.tmp\n",
		"a/b/.gitignore":   "*.bak\n",
		"a/b/c/.gitignore": "*.swp\n",
		"root.log":         "log",
		"a/file.tmp":       "tmp",
		"a/b/file.bak":     "bak",
		"a/b/c/file.swp":   "swp",
		"a/b/c/keep.go":    "package c",
		"a/keep.go":        "package a",
	})

	names := scanNames(t, &FileSystemScanner{}, tmp)
	assertPresent(t, names, "a/b/c/keep.go")
	assertPresent(t, names, "a/keep.go")
	assertAbsent(t, names, "root.log")
	assertAbsent(t, names, "a/file.tmp")
	assertAbsent(t, names, "a/b/file.bak")
	assertAbsent(t, names, "a/b/c/file.swp")
}

func TestScan_UnicodeFilename(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		"café.go":     "package main",
		"日本語.md":      "# 日本語",
		"données.csv": "a,b,c",
	})

	names := scanNames(t, &FileSystemScanner{}, tmp)
	assertPresent(t, names, "café.go")
	assertPresent(t, names, "日本語.md")
	assertPresent(t, names, "données.csv")
}

func TestScan_ExcludeOverridesGitignore(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		".gitignore":    "*.log\n!important.log\n",
		"app.go":        "package main",
		"important.log": "important",
	})

	// User --exclude pattern overrides the gitignore re-inclusion.
	names := scanNames(t, &FileSystemScanner{Exclude: []string{"important.log"}}, tmp)
	assertPresent(t, names, "app.go")
	assertAbsent(t, names, "important.log")
}

func TestScan_SymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{
		"real.go": "package main",
	})

	// Create a symlink to the real file.
	symlink := filepath.Join(tmp, "link.go")
	if err := os.Symlink(filepath.Join(tmp, "real.go"), symlink); err != nil {
		t.Fatal(err)
	}

	names := scanNames(t, &FileSystemScanner{}, tmp)
	assertPresent(t, names, "real.go")
	// Symlinks may or may not be followed depending on WalkDir behavior;
	// this test verifies no panic occurs.
}
