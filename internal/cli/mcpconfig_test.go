package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func jsonDoc(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMergeJSONServers_PreservesExistingAndAddsKg(t *testing.T) {
	existing := jsonDoc(t, map[string]any{
		"mcpServers": map[string]any{
			"playwright": map[string]any{"command": "npx", "args": []string{"-y", "@playwright/mcp"}},
		},
	})
	mc := mcpConfig{paths: []string{".mcp.json"}, kind: "json"}

	out, err := mergeJSONServers([]byte(existing), mc, "kg", "graph.json")
	if err != nil {
		t.Fatalf("mergeJSONServers returned error: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("merged output is not valid JSON: %v\n%s", err, out)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		t.Fatalf("mcpServers missing:\n%s", out)
	}
	if _, ok := servers["playwright"]; !ok {
		t.Errorf("existing playwright server was dropped:\n%s", out)
	}
	kg, _ := servers["kg"].(map[string]any)
	if kg == nil {
		t.Fatalf("kg entry missing:\n%s", out)
	}
	if kg["command"] != "kg" {
		t.Errorf("kg command = %v, want kg", kg["command"])
	}
	args, _ := kg["args"].([]any)
	if len(args) != 3 || args[2] != "graph.json" {
		t.Errorf("kg args = %v, want [mcp --file graph.json]", args)
	}
}

func TestMergeJSONServers_EmptyFileBecomesServerDoc(t *testing.T) {
	mc := mcpConfig{paths: []string{".mcp.json"}, kind: "json"}
	out, err := mergeJSONServers(nil, mc, "kg", "graph.json")
	if err != nil {
		t.Fatalf("mergeJSONServers returned error: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if _, ok := doc["mcpServers"].(map[string]any)["kg"]; !ok {
		t.Errorf("kg entry missing in empty-file merge:\n%s", out)
	}
}

func TestMergeJSONServers_IdempotentWhenUnchanged(t *testing.T) {
	mc := mcpConfig{paths: []string{".mcp.json"}, kind: "json"}
	first, err := mergeJSONServers(nil, mc, "kg", "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	second, err := mergeJSONServers(first, mc, "kg", "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Errorf("re-merge changed output:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestMergeJSONServers_ReplacesKgOnlyOnGraphChange(t *testing.T) {
	mc := mcpConfig{paths: []string{".mcp.json"}, kind: "json"}
	first, _ := mergeJSONServers(nil, mc, "kg", "graph.json")
	second, err := mergeJSONServers(first, mc, "kg", "out/kg.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(second), "graph.json") {
		t.Errorf("old graph path not replaced:\n%s", second)
	}
	if !strings.Contains(string(second), "out/kg.json") {
		t.Errorf("new graph path missing:\n%s", second)
	}
}

func TestMergeJSONServers_InvalidExistingIsError(t *testing.T) {
	mc := mcpConfig{paths: []string{".mcp.json"}, kind: "json"}
	if _, err := mergeJSONServers([]byte("{not json"), mc, "kg", "graph.json"); err == nil {
		t.Error("expected an error for invalid existing JSON")
	}
}

func TestMergeOpenCode_StrictJSON(t *testing.T) {
	existing := jsonDoc(t, map[string]any{
		"$schema": opencodeSchema,
		"mcp": map[string]any{
			"jira": map[string]any{"type": "remote", "url": "https://jira.example.com/mcp"},
		},
	})
	out, err := mergeOpenCode([]byte(existing), "kg", "graph.json")
	if err != nil {
		t.Fatalf("mergeOpenCode returned error: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	mcp, _ := doc["mcp"].(map[string]any)
	if _, ok := mcp["jira"]; !ok {
		t.Errorf("existing jira server dropped:\n%s", out)
	}
	kg, _ := mcp["kg"].(map[string]any)
	if kg["type"] != "local" || kg["cwd"] != "." {
		t.Errorf("kg entry = %v, want local server rooted at the project", kg)
	}
	if doc["$schema"] != opencodeSchema {
		t.Errorf("$schema missing:\n%s", out)
	}
}

func TestMergeOpenCode_JSONCWithExistingMcp(t *testing.T) {
	existing := `{
  // project-level opencode config
  "mcp": {
    "jira": { "type": "remote", "url": "https://jira.example.com/mcp" },
  }, // keep this comment
}`
	out, err := mergeOpenCode([]byte(existing), "kg", "graph.json")
	if err != nil {
		t.Fatalf("mergeOpenCode returned error: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "keep this comment") {
		t.Errorf("jsonc comments were lost:\n%s", got)
	}
	if !strings.Contains(got, `"jira"`) {
		t.Errorf("existing jira server was dropped:\n%s", got)
	}
	if !strings.Contains(got, `"kg"`) || !strings.Contains(got, `"mcp", "--file", "graph.json"`) {
		t.Errorf("kg entry not injected:\n%s", got)
	}
	if strings.Count(got, `"mcp":`) != 1 {
		t.Errorf("duplicate mcp key introduced:\n%s", got)
	}
}

func TestMergeOpenCode_JSONCWithoutMcp(t *testing.T) {
	existing := "{\n  \"model\": \"anthropic/claude\",\n} // trailing comment\n"
	out, err := mergeOpenCode([]byte(existing), "kg", "graph.json")
	if err != nil {
		t.Fatalf("mergeOpenCode returned error: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, `"model"`) {
		t.Errorf("existing key dropped:\n%s", got)
	}
	if !strings.Contains(got, "trailing comment") {
		t.Errorf("trailing comment lost:\n%s", got)
	}
	if !strings.Contains(got, `"mcp": {`) || !strings.Contains(got, `"kg"`) {
		t.Errorf("mcp.kg not injected:\n%s", got)
	}
}

func TestMergeOpenCode_JSONCIdempotent(t *testing.T) {
	existing := "{\n  \"mcp\": {\n    \"jira\": { \"type\": \"remote\", \"url\": \"https://x.example/mcp\" },\n  },\n}\n"
	first, err := mergeOpenCode([]byte(existing), "kg", "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	second, err := mergeOpenCode(first, "kg", "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Errorf("jsonc re-merge changed output:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestMergeTOML(t *testing.T) {
	mc := mcpConfig{paths: []string{".codex/config.toml"}, kind: "codex", cwd: true}
	existing := "[projects.\"/repo\"]\ntrust_level = \"trusted\"\n"
	out, err := mergeTOML([]byte(existing), mc, "kg", "graph.json")
	if err != nil {
		t.Fatalf("mergeTOML returned error: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, `trust_level`) {
		t.Errorf("existing TOML content lost:\n%s", got)
	}
	if !strings.Contains(got, "[mcp_servers.kg]") {
		t.Errorf("mcp_servers.kg table missing:\n%s", got)
	}
	if !strings.Contains(got, `args = ['mcp', '--file', 'graph.json']`) {
		t.Errorf("kg args missing:\n%s", got)
	}

	again, err := mergeTOML(out, mc, "kg", "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(out) {
		t.Errorf("toml re-merge changed output:\n%s\nvs\n%s", out, again)
	}
}

func TestMergeTOML_InvalidExistingIsError(t *testing.T) {
	mc := mcpConfig{paths: []string{".codex/config.toml"}, kind: "codex"}
	if _, err := mergeTOML([]byte("not = [valid"), mc, "kg", "graph.json"); err == nil {
		t.Error("expected an error for invalid existing TOML")
	}
}

func TestWriteMCPConfig_CreateUpdateUnchanged(t *testing.T) {
	dir := t.TempDir()
	mc := mcpConfig{paths: []string{".mcp.json"}, kind: "json"}

	status, err := writeMCPConfig(dir, mc, "kg", "graph.json")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if status != "created" {
		t.Errorf("status = %q, want created", status)
	}
	data := readFile(t, filepath.Join(dir, ".mcp.json"))
	if !strings.Contains(data, `"kg"`) {
		t.Errorf("fresh config missing kg:\n%s", data)
	}

	status, err = writeMCPConfig(dir, mc, "kg", "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	if status != "unchanged" {
		t.Errorf("status = %q, want unchanged", status)
	}

	status, err = writeMCPConfig(dir, mc, "kg", "out.json")
	if err != nil {
		t.Fatal(err)
	}
	if status != "updated" {
		t.Errorf("status = %q, want updated", status)
	}
}

func TestWriteMCPConfig_PrefersExistingCandidate(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"opencode.jsonc": "{\n  // jsonc\n  \"model\": \"x\",\n}\n",
	})
	mc := mcpConfig{paths: []string{"opencode.json", "opencode.jsonc"}, kind: "opencode"}

	status, err := writeMCPConfig(dir, mc, "kg", "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	if status != "updated" {
		t.Errorf("status = %q, want updated (merged into jsonc)", status)
	}
	if _, err := os.Stat(filepath.Join(dir, "opencode.json")); !os.IsNotExist(err) {
		t.Error("opencode.json should not be created when opencode.jsonc exists")
	}
	got := readFile(t, filepath.Join(dir, "opencode.jsonc"))
	if !strings.Contains(got, "// jsonc") || !strings.Contains(got, `"kg"`) {
		t.Errorf("jsonc merge failed:\n%s", got)
	}
}

func TestInitCmd_WritesMCPConfigsForTargets(t *testing.T) {
	dir := t.TempDir()
	resetInitFlags()
	defer resetInitFlags()
	initMCPCmd = "kg"

	if err := initCmd.ParseFlags([]string{"--targets=claude,codex,opencode,copilot,gemini"}); err != nil {
		t.Fatal(err)
	}
	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("init returned error: %v", err)
	}

	cases := []struct{ rel, want string }{
		{".mcp.json", `"kg"`},
		{filepath.Join(".codex", "config.toml"), "[mcp_servers.kg]"},
		{"opencode.json", `"kg"`},
		{filepath.Join(".github", "mcp.json"), `"tools"`},
		{filepath.Join(".gemini", "settings.json"), `"cwd"`},
	}
	for _, c := range cases {
		content := readFile(t, filepath.Join(dir, c.rel))
		if !strings.Contains(content, c.want) {
			t.Errorf("%s missing %q:\n%s", c.rel, c.want, content)
		}
	}
}

func TestInitCmd_MCPConfigsIdempotentAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	resetInitFlags()
	defer resetInitFlags()
	initMCPCmd = "kg"
	if err := initCmd.ParseFlags([]string{"--targets=claude"}); err != nil {
		t.Fatal(err)
	}
	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, filepath.Join(dir, ".mcp.json"))
	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatal(err)
	}
	second := readFile(t, filepath.Join(dir, ".mcp.json"))
	if second != first {
		t.Errorf("re-run changed .mcp.json:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestInitCmd_NoMCPFlagSkipsConfigFiles(t *testing.T) {
	dir := t.TempDir()
	resetInitFlags()
	defer resetInitFlags()
	initMCPCmd = "kg"
	if err := initCmd.ParseFlags([]string{"--targets=claude", "--no-mcp"}); err != nil {
		t.Fatal(err)
	}
	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("init returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Error(".mcp.json should not be created with --no-mcp")
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Errorf("CLAUDE.md rule should still be written: %v", err)
	}
}

func TestInitCmd_MergesExistingConfigWithoutClobbering(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".mcp.json": jsonDoc(t, map[string]any{
			"mcpServers": map[string]any{
				"playwright": map[string]any{"command": "npx", "args": []string{"-y", "@playwright/mcp"}},
			},
		}),
	})
	resetInitFlags()
	defer resetInitFlags()
	initMCPCmd = "kg"
	if err := initCmd.ParseFlags([]string{"--targets=claude"}); err != nil {
		t.Fatal(err)
	}
	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("init returned error: %v", err)
	}

	content := readFile(t, filepath.Join(dir, ".mcp.json"))
	if !strings.Contains(content, "playwright") {
		t.Errorf("existing playwright server was clobbered:\n%s", content)
	}
	if !strings.Contains(content, `"kg"`) {
		t.Errorf("kg entry missing:\n%s", content)
	}
}

func TestInitCmd_UnparseableConfigLeftUntouched(t *testing.T) {
	dir := t.TempDir()
	broken := "{ this is not valid json"
	writeTree(t, dir, map[string]string{".mcp.json": broken})
	resetInitFlags()
	defer resetInitFlags()
	initMCPCmd = "kg"
	if err := initCmd.ParseFlags([]string{"--targets=claude"}); err != nil {
		t.Fatal(err)
	}
	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("init returned error: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, ".mcp.json")); got != broken {
		t.Errorf("broken config was modified:\n%s", got)
	}
}

func TestResolveTargets_DetectsOpenCodeMarker(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"opencode.jsonc": "{}"})
	got, err := resolveTargets(nil, false, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(got, "opencode") {
		t.Errorf("detected targets = %v, want opencode", got)
	}
}

func TestInitCmd_OpenCodeTargetWiresAGENTSAndConfig(t *testing.T) {
	dir := t.TempDir()
	resetInitFlags()
	defer resetInitFlags()
	initMCPCmd = "kg"
	if err := initCmd.ParseFlags([]string{"--targets=opencode"}); err != nil {
		t.Fatal(err)
	}
	if err := initCmd.RunE(initCmd, []string{dir}); err != nil {
		t.Fatalf("init returned error: %v", err)
	}

	content := readFile(t, filepath.Join(dir, "opencode.json"))
	var doc map[string]any
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatalf("opencode.json not valid JSON: %v\n%s", err, content)
	}
	kg, _ := doc["mcp"].(map[string]any)["kg"].(map[string]any)
	if kg == nil {
		t.Fatalf("opencode.json missing mcp.kg:\n%s", content)
	}
	if kg["type"] != "local" || kg["cwd"] != "." {
		t.Errorf("kg entry = %v, want project-root local server", kg)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md rule not written: %v", err)
	}
	agents := readFile(t, filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(agents, "opencode.json") {
		t.Errorf("AGENTS.md missing MCP wiring note:\n%s", agents)
	}
}

func TestMCPWiringNote_WindsurfDocumentsGlobalConfig(t *testing.T) {
	note := mcpWiringNote("windsurf", "graph.json")
	if !strings.Contains(note, "~/.codeium/windsurf/mcp_config.json") {
		t.Errorf("windsurf note missing global config path:\n%s", note)
	}
	if !strings.Contains(note, `"args": ["mcp", "--file", "graph.json"]`) {
		t.Errorf("windsurf note missing kg server snippet:\n%s", note)
	}
}

func TestMCPWiringNote_AgentsHasNoConfig(t *testing.T) {
	if note := mcpWiringNote("agents", "graph.json"); note != "" {
		t.Errorf("agents should have no wiring note, got:\n%s", note)
	}
}
