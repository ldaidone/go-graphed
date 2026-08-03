package cli

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func resetInitFlags() {
	initGraphFile = ""
	initAll = false
	initTargets = nil
	initBuild = false
}

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

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}

func TestResolveTargets_ExplicitValidates(t *testing.T) {
	got, err := resolveTargets([]string{"claude,gemini"}, false, t.TempDir())
	if err != nil {
		t.Fatalf("resolveTargets returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"agents", "claude", "gemini"}) {
		t.Errorf("resolveTargets() = %v, want [agents claude gemini]", got)
	}
}

func TestResolveTargets_AlwaysIncludesAgents(t *testing.T) {
	got, err := resolveTargets(nil, false, t.TempDir())
	if err != nil {
		t.Fatalf("resolveTargets returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"agents"}) {
		t.Errorf("resolveTargets() on empty dir = %v, want [agents]", got)
	}
}

func TestResolveTargets_UnknownErrors(t *testing.T) {
	if _, err := resolveTargets([]string{"bogus"}, false, t.TempDir()); err == nil {
		t.Error("resolveTargets with unknown target should error")
	}
}

func TestResolveTargets_DetectsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"CLAUDE.md":                       "dev claude rules",
		"GEMINI.md":                       "dev gemini rules",
		".github/copilot-instructions.md": "dev copilot rules",
		".cursor/rules/something.mdc":     "existing cursor rule",
		".clinerules":                     "",
		".windsurf/rules/other.mdc":       "existing windsurf rule",
		"CODEASSIST.md":                   "dev codeassist rules",
	})

	got, err := resolveTargets(nil, false, dir)
	if err != nil {
		t.Fatalf("resolveTargets returned error: %v", err)
	}
	want := []string{"agents", "claude", "gemini", "copilot", "cursor", "cline", "windsurf"}
	for _, w := range want {
		if !contains(got, w) {
			t.Errorf("detectTargets() = %v, missing %q", got, w)
		}
	}
}

func TestInitCmd_CreatesAgentsMdWhenMissing(t *testing.T) {
	dir := t.TempDir()
	resetInitFlags()
	defer resetInitFlags()

	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("init returned error: %v", err)
	}

	content := readFile(t, filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(content, "get_narrowed_context") {
		t.Errorf("AGENTS.md missing workflow content:\n%s", content)
	}
	if !strings.HasPrefix(content, "# Project Guidance for AI Agents") {
		t.Errorf("AGENTS.md should start with the title:\n%s", content)
	}
}

func TestInitCmd_AppendsToExistingFileKeepingDeveloperContent(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"CLAUDE.md": "# Developer Rules\n\nNever touch the Makefile.\n",
	})
	resetInitFlags()
	defer resetInitFlags()

	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("init returned error: %v", err)
	}

	content := readFile(t, filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(content, "Never touch the Makefile.") {
		t.Errorf("developer content was lost:\n%s", content)
	}
	if !strings.Contains(content, "get_narrowed_context") {
		t.Errorf("kg block not appended:\n%s", content)
	}

	// AGENTS.md (the default rule) is created even though only Claude was
	// detected.
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md should be created: %v", err)
	}
}

func TestInitCmd_AppendsForEveryDetectedTool(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"CLAUDE.md":                       "claude dev rules\n",
		"GEMINI.md":                       "gemini dev rules\n",
		".github/copilot-instructions.md": "copilot dev rules\n",
	})
	resetInitFlags()
	defer resetInitFlags()

	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("init returned error: %v", err)
	}

	for _, rel := range []string{
		"CLAUDE.md",
		"GEMINI.md",
		filepath.Join(".github", "copilot-instructions.md"),
	} {
		content := readFile(t, filepath.Join(dir, rel))
		if !strings.Contains(content, "get_narrowed_context") {
			t.Errorf("%s missing kg block", rel)
		}
	}

	// Targets not in use must not be created.
	if _, err := os.Stat(filepath.Join(dir, ".cursor", "rules", "kg.mdc")); !os.IsNotExist(err) {
		t.Error("cursor rule should not be created when Cursor is not detected")
	}
}

func TestInitCmd_IdempotentAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"CLAUDE.md": "# Developer Rules\n",
	})
	resetInitFlags()
	defer resetInitFlags()

	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("first init returned error: %v", err)
	}
	first := readFile(t, filepath.Join(dir, "CLAUDE.md"))

	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("second init returned error: %v", err)
	}
	second := readFile(t, filepath.Join(dir, "CLAUDE.md"))

	if strings.Count(second, rulesStart) != 1 {
		t.Errorf("kg block duplicated across runs:\n%s", second)
	}
	if !strings.Contains(second, "# Developer Rules") {
		t.Errorf("developer content lost on re-run:\n%s", second)
	}
	if second != first {
		t.Errorf("re-run should be a no-op, got diff:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestInitCmd_ExplicitTargetCreatesMdcWithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	resetInitFlags()
	defer resetInitFlags()
	if err := initCmd.ParseFlags([]string{"--targets=cursor"}); err != nil {
		t.Fatal(err)
	}

	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("init returned error: %v", err)
	}

	target := filepath.Join(dir, ".cursor", "rules", "kg.mdc")
	content := readFile(t, target)
	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("cursor rule missing .mdc frontmatter:\n%s", content)
	}
	if !strings.Contains(content, "get_narrowed_context") {
		t.Errorf("cursor rule missing workflow content:\n%s", content)
	}
}

func TestInitCmd_MdcAppendPreservesFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".cursor/rules/kg.mdc": "---\ndescription: my own cursor rules\n---\n\nMy dev rules.\n",
	})
	resetInitFlags()
	defer resetInitFlags()
	if err := initCmd.ParseFlags([]string{"--targets=cursor"}); err != nil {
		t.Fatal(err)
	}

	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("init returned error: %v", err)
	}

	content := readFile(t, filepath.Join(dir, ".cursor", "rules", "kg.mdc"))
	if !strings.HasPrefix(content, "---\ndescription: my own cursor rules\n") {
		t.Errorf("existing .mdc frontmatter was not preserved:\n%s", content)
	}
	if !strings.Contains(content, "My dev rules.") {
		t.Errorf("developer content lost:\n%s", content)
	}
	if !strings.Contains(content, "get_narrowed_context") {
		t.Errorf("kg block not appended:\n%s", content)
	}
}

// isolateInitEnv points HOME/cwd at isolated temp dirs and clears every
// config source so a build inside init cannot pick up the repo's own model.
func isolateInitEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GRAPHEAD_MODEL_PATH", "")
	t.Setenv("GRAPHEAD_DB_ROOT", "")
	t.Setenv("GRAPHEAD_DIMENSIONS", "")
	t.Setenv("GRAPHEAD_CONFIG_FILE", "")
	t.Chdir(t.TempDir())
}

func TestInitCmd_BuildFlagProducesGraph(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.go": "package main\n\ntype Animal interface{ Speak() string }\n",
	})
	resetInitFlags()
	defer resetInitFlags()
	isolateInitEnv(t)
	initBuild = true

	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("init --build returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "graph.json")); err != nil {
		t.Errorf("graph.json not produced by init --build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md not written: %v", err)
	}
}

func TestInitCmd_MissingGraphPrintsWarning(t *testing.T) {
	dir := t.TempDir() // no graph.json
	resetInitFlags()
	defer resetInitFlags()

	out := captureStdout(t, func() {
		if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
			t.Fatalf("init returned error: %v", err)
		}
	})
	if !strings.Contains(out, "not found") || !strings.Contains(out, "kg build") {
		t.Errorf("expected a missing-graph warning, got:\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
