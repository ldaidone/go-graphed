package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveModelPath_ExplicitWins(t *testing.T) {
	t.Setenv(EnvModelPath, "/env/model.gtemodel")
	got := ResolveModelPath("/explicit/model.gtemodel")
	if got != "/explicit/model.gtemodel" {
		t.Errorf("ResolveModelPath(explicit) = %q, want the explicit path", got)
	}
}

func TestResolveModelPath_EnvFallback(t *testing.T) {
	t.Setenv(EnvModelPath, "/env/model.gtemodel")
	t.Chdir(t.TempDir()) // no get-small.gtemodel in cwd
	got := ResolveModelPath("")
	if got != "/env/model.gtemodel" {
		t.Errorf("ResolveModelPath() = %q, want env value", got)
	}
}

func TestResolveModelPath_CwdFallback(t *testing.T) {
	t.Setenv(EnvModelPath, "") // clear env so the cwd fallback is exercised
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, ModelFileName), []byte("model"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tmp)

	got := ResolveModelPath("")
	want := filepath.Join(tmp, ModelFileName)
	if got != want {
		t.Errorf("ResolveModelPath() = %q, want cwd fallback %q", got, want)
	}
}

func TestResolveModelPath_EmptyWhenNothingConfigured(t *testing.T) {
	t.Setenv(EnvModelPath, "")
	t.Chdir(t.TempDir()) // no model file in cwd
	if got := ResolveModelPath(""); got != "" {
		t.Errorf("ResolveModelPath() = %q, want empty (disabled)", got)
	}
}

func TestBadgerDBPath_DefaultRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := BadgerDBPath("")
	if err != nil {
		t.Fatalf("BadgerDBPath returned error: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "graphed", filepath.Base(cwd))
	if got != want {
		t.Errorf("BadgerDBPath() = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("expected db directory to exist: %v", err)
	}
}

func TestBadgerDBPath_CustomRoot(t *testing.T) {
	root := t.TempDir()

	got, err := BadgerDBPath(root)
	if err != nil {
		t.Fatalf("BadgerDBPath returned error: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, filepath.Base(cwd))
	if got != want {
		t.Errorf("BadgerDBPath(root) = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("expected db directory to exist: %v", err)
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv(EnvModelPath, "")
	t.Setenv(EnvDBRoot, "")
	t.Setenv(EnvDimensions, "")
	t.Chdir(t.TempDir())

	s, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.Dimensions != 0 {
		t.Errorf("Dimensions = %d, want 0 (auto-detect)", s.Dimensions)
	}
	if s.ModelPath != "" {
		t.Errorf("ModelPath = %q, want empty", s.ModelPath)
	}
	if s.DBRoot != "" {
		t.Errorf("DBRoot = %q, want empty (default resolved lazily)", s.DBRoot)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv(EnvModelPath, "/env/model.gtemodel")
	t.Setenv(EnvDBRoot, "/env/dbroot")
	t.Setenv(EnvDimensions, "512")

	s, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.Dimensions != 512 {
		t.Errorf("Dimensions = %d, want 512", s.Dimensions)
	}
	if s.ModelPath != "/env/model.gtemodel" {
		t.Errorf("ModelPath = %q, want env value", s.ModelPath)
	}
	if s.DBRoot != "/env/dbroot" {
		t.Errorf("DBRoot = %q, want env value", s.DBRoot)
	}
}

func TestLoad_ExplicitOverridesEnv(t *testing.T) {
	t.Setenv(EnvModelPath, "/env/model.gtemodel")
	t.Setenv(EnvDBRoot, "/env/dbroot")
	t.Setenv(EnvDimensions, "512")

	s, err := Load(Overrides{
		ModelPath:  "/flag/model.gtemodel",
		Dimensions: 768,
		DBRoot:     "/flag/dbroot",
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.Dimensions != 768 {
		t.Errorf("Dimensions = %d, want 768", s.Dimensions)
	}
	if s.ModelPath != "/flag/model.gtemodel" {
		t.Errorf("ModelPath = %q, want flag value", s.ModelPath)
	}
	if s.DBRoot != "/flag/dbroot" {
		t.Errorf("DBRoot = %q, want flag value", s.DBRoot)
	}
}

func TestLoad_InvalidEnvDimensionsFallsBack(t *testing.T) {
	t.Setenv(EnvDimensions, "not-a-number")

	s, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.Dimensions != 0 {
		t.Errorf("Dimensions = %d, want 0 (auto-detect fallback)", s.Dimensions)
	}
}

// isolateConfigFiles resets every source that could leak between tests and
// points HOME/cwd at isolated temp dirs.
func isolateConfigFiles(t *testing.T, home string) {
	t.Helper()
	t.Setenv(EnvModelPath, "")
	t.Setenv(EnvDBRoot, "")
	t.Setenv(EnvDimensions, "")
	t.Setenv(EnvConfigFile, "")
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())
}

func TestLoad_ConfigFileYAML(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "graphed.yaml"), []byte(`
model_path: /file/model.gtemodel
db_root: /file/dbroot
dimensions: 512
`), 0644); err != nil {
		t.Fatal(err)
	}
	isolateConfigFiles(t, home)
	t.Chdir(dir)

	s, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.ModelPath != "/file/model.gtemodel" {
		t.Errorf("ModelPath = %q, want config file value", s.ModelPath)
	}
	if s.DBRoot != "/file/dbroot" {
		t.Errorf("DBRoot = %q, want config file value", s.DBRoot)
	}
	if s.Dimensions != 512 {
		t.Errorf("Dimensions = %d, want 512", s.Dimensions)
	}
}

func TestLoad_ConfigFileJSONExplicit(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "custom.json")
	if err := os.WriteFile(cfg, []byte(`{"model_path": "/file/model.gtemodel", "db_root": "/file/dbroot", "dimensions": 768}`), 0644); err != nil {
		t.Fatal(err)
	}
	isolateConfigFiles(t, home)
	t.Setenv(EnvConfigFile, cfg)

	s, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.ModelPath != "/file/model.gtemodel" {
		t.Errorf("ModelPath = %q, want config file value", s.ModelPath)
	}
	if s.Dimensions != 768 {
		t.Errorf("Dimensions = %d, want 768", s.Dimensions)
	}
}

func TestLoad_ConfigFileTOML(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "graphed.toml"), []byte(`
model_path = "/file/model.gtemodel"
db_root = "/file/dbroot"
dimensions = 256
`), 0644); err != nil {
		t.Fatal(err)
	}
	isolateConfigFiles(t, home)
	t.Chdir(dir)

	s, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.ModelPath != "/file/model.gtemodel" {
		t.Errorf("ModelPath = %q, want config file value", s.ModelPath)
	}
	if s.Dimensions != 256 {
		t.Errorf("Dimensions = %d, want 256", s.Dimensions)
	}
}

func TestLoad_DotGraphedConfigFile(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".graphed.yaml"), []byte("model_path: /file/model.gtemodel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	isolateConfigFiles(t, home)
	t.Chdir(dir)

	s, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.ModelPath != "/file/model.gtemodel" {
		t.Errorf("ModelPath = %q, want config file value", s.ModelPath)
	}
}

func TestLoad_UserConfigDir(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".config", "graphed", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("model_path: /home/model.gtemodel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	isolateConfigFiles(t, home)

	s, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.ModelPath != "/home/model.gtemodel" {
		t.Errorf("ModelPath = %q, want user config value", s.ModelPath)
	}
}

func TestLoad_DotEnvBeatsConfigFile(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "graphed.yaml"), []byte("model_path: /file/model.gtemodel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GRAPHEAD_MODEL_PATH=/envfile/model.gtemodel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	isolateConfigFiles(t, home)
	t.Chdir(dir)

	s, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.ModelPath != "/envfile/model.gtemodel" {
		t.Errorf("ModelPath = %q, want .env value", s.ModelPath)
	}
}

func TestLoad_EnvBeatsDotEnv(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GRAPHEAD_MODEL_PATH=/envfile/model.gtemodel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	isolateConfigFiles(t, home)
	t.Chdir(dir)
	t.Setenv(EnvModelPath, "/env/model.gtemodel")

	s, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.ModelPath != "/env/model.gtemodel" {
		t.Errorf("ModelPath = %q, want environment value", s.ModelPath)
	}
}

func TestLoad_OverrideBeatsEverything(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "graphed.yaml"), []byte("model_path: /file/model.gtemodel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GRAPHEAD_MODEL_PATH=/envfile/model.gtemodel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	isolateConfigFiles(t, home)
	t.Chdir(dir)
	t.Setenv(EnvModelPath, "/env/model.gtemodel")

	s, err := Load(Overrides{ModelPath: "/flag/model.gtemodel"})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if s.ModelPath != "/flag/model.gtemodel" {
		t.Errorf("ModelPath = %q, want override value", s.ModelPath)
	}
}

func TestResolveModelPath_ConfigFileFallback(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "graphed.yaml"), []byte("model_path: /file/model.gtemodel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	isolateConfigFiles(t, home)
	t.Chdir(dir)

	if got := ResolveModelPath(""); got != "/file/model.gtemodel" {
		t.Errorf("ResolveModelPath() = %q, want config file value", got)
	}
}

func TestParseDimensions_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fallback int
		want     int
	}{
		{name: "valid positive", input: "384", fallback: 0, want: 384},
		{name: "non-numeric", input: "abc", fallback: 0, want: 0},
		{name: "empty", input: "", fallback: 0, want: 0},
		{name: "zero falls back", input: "0", fallback: 384, want: 384},
		{name: "negative falls back", input: "-12", fallback: 384, want: 384},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseDimensions(tt.input, tt.fallback); got != tt.want {
				t.Errorf("parseDimensions(%q, %d) = %d, want %d", tt.input, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestUserConfigDir_EmptyHome(t *testing.T) {
	t.Setenv("HOME", "")
	if got := UserConfigDir(); got != "" {
		t.Errorf("UserConfigDir() = %q, want empty when HOME is unset", got)
	}
}

func TestBadgerDBPath_ErrorPaths(t *testing.T) {
	t.Run("no home directory", func(t *testing.T) {
		t.Setenv("HOME", "")
		if _, err := BadgerDBPath(""); err == nil {
			t.Error("expected error when HOME is unset")
		}
	})

	t.Run("root is a file", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := BadgerDBPath(blocker); err == nil {
			t.Error("expected error when DBRoot is a file")
		}
	})
}
