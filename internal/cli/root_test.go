package cli

import (
	"testing"
)

func TestRootCmd_HasSubcommands(t *testing.T) {
	for _, name := range []string{"build", "mcp", "init", "install"} {
		cmd, _, err := RootCmd.Find([]string{name})
		if err != nil || cmd == nil {
			t.Errorf("root command missing subcommand %q", name)
		}
	}
}

func TestRootCmd_GroupsRegistered(t *testing.T) {
	found := map[string]bool{}
	for _, g := range RootCmd.Groups() {
		found[g.ID] = true
	}
	for _, id := range []string{"core", "infra", "setup"} {
		if !found[id] {
			t.Errorf("root command missing group %q", id)
		}
	}
}

func TestBuildCmd_FlagDefaults(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"build"})
	if err != nil || cmd == nil {
		t.Fatal("build command not found")
	}
	output, err := cmd.Flags().GetString("output")
	if err != nil {
		t.Fatalf("output flag: %v", err)
	}
	if output != "graph.json" {
		t.Errorf("output default = %q, want %q", output, "graph.json")
	}
	format, err := cmd.Flags().GetString("format")
	if err != nil {
		t.Fatalf("format flag: %v", err)
	}
	if format != "json" {
		t.Errorf("format default = %q, want %q", format, "json")
	}

	modelPath, err := cmd.Flags().GetString("model-path")
	if err != nil {
		t.Fatalf("model-path flag: %v", err)
	}
	if modelPath != "" {
		t.Errorf("model-path default = %q, want empty", modelPath)
	}

	dimensions, err := cmd.Flags().GetInt("dimensions")
	if err != nil {
		t.Fatalf("dimensions flag: %v", err)
	}
	if dimensions != 0 {
		t.Errorf("dimensions default = %d, want 0 (auto-detect)", dimensions)
	}

	dbRoot, err := cmd.Flags().GetString("db-root")
	if err != nil {
		t.Fatalf("db-root flag: %v", err)
	}
	if dbRoot != "" {
		t.Errorf("db-root default = %q, want empty", dbRoot)
	}
}

func TestMCPCmd_FlagDefaults(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"mcp"})
	if err != nil || cmd == nil {
		t.Fatal("mcp command not found")
	}
	file, err := cmd.Flags().GetString("file")
	if err != nil {
		t.Fatalf("file flag: %v", err)
	}
	if file != "graph.json" {
		t.Errorf("file default = %q, want %q", file, "graph.json")
	}

	modelPath, err := cmd.Flags().GetString("model-path")
	if err != nil {
		t.Fatalf("model-path flag: %v", err)
	}
	if modelPath != "" {
		t.Errorf("model-path default = %q, want empty", modelPath)
	}

	dbRoot, err := cmd.Flags().GetString("db-root")
	if err != nil {
		t.Fatalf("db-root flag: %v", err)
	}
	if dbRoot != "" {
		t.Errorf("db-root default = %q, want empty", dbRoot)
	}
}

func TestBuildCmd_RequiresSourceArg(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"build"})
	if err != nil || cmd == nil {
		t.Fatal("build command not found")
	}
	if err := cmd.ValidateArgs(nil); err == nil {
		t.Error("build without source argument should fail validation")
	}
	if err := cmd.ValidateArgs([]string{"."}); err != nil {
		t.Errorf("build with source argument should pass validation, got: %v", err)
	}
}
