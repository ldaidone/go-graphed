package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/config"
)

func TestDefaultBinDir_GOBIN(t *testing.T) {
	t.Setenv("GOBIN", "/x/bin")
	t.Setenv("GOPATH", "/g")
	t.Setenv("HOME", t.TempDir())
	if got := defaultBinDir(); got != "/x/bin" {
		t.Errorf("defaultBinDir() = %q, want GOBIN value", got)
	}
}

func TestDefaultBinDir_GOPATH(t *testing.T) {
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "/g")
	t.Setenv("HOME", t.TempDir())
	if got := defaultBinDir(); got != filepath.Join("/g", "bin") {
		t.Errorf("defaultBinDir() = %q, want GOPATH/bin", got)
	}
}

func TestDefaultBinDir_HomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".local", "bin")
	if got := defaultBinDir(); got != want {
		t.Errorf("defaultBinDir() = %q, want %q", got, want)
	}
}

func TestInstallBinary(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(src, []byte("binary-content"), 0644); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()

	dest, err := installBinary(src, binDir)
	if err != nil {
		t.Fatalf("installBinary returned error: %v", err)
	}
	want := filepath.Join(binDir, "kg")
	if dest != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("installed binary not readable: %v", err)
	}
	if string(data) != "binary-content" {
		t.Errorf("installed binary content = %q, want %q", data, "binary-content")
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("installed binary mode = %o, want 755", info.Mode().Perm())
	}
}

func TestInstallModel(t *testing.T) {
	src := filepath.Join(t.TempDir(), config.ModelFileName)
	if err := os.WriteFile(src, []byte("model-content"), 0644); err != nil {
		t.Fatal(err)
	}
	modelDir := t.TempDir()

	dest, err := installModel(src, modelDir)
	if err != nil {
		t.Fatalf("installModel returned error: %v", err)
	}
	want := filepath.Join(modelDir, config.ModelFileName)
	if dest != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("installed model not readable: %v", err)
	}
	if string(data) != "model-content" {
		t.Errorf("installed model content = %q, want %q", data, "model-content")
	}
}

func TestEnsureUserConfig_WritesWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := ensureUserConfig("/models/get-small.gtemodel"); err != nil {
		t.Fatalf("ensureUserConfig returned error: %v", err)
	}

	cfg := filepath.Join(home, ".config", "graphed", "config.yaml")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if !strings.Contains(string(data), "model_path: /models/get-small.gtemodel") {
		t.Errorf("config file content = %q, want model_path line", data)
	}
}

func TestEnsureUserConfig_KeepsExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := filepath.Join(home, ".config", "graphed", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0755); err != nil {
		t.Fatal(err)
	}
	original := "db_root: /custom\n"
	if err := os.WriteFile(cfg, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureUserConfig("/models/get-small.gtemodel"); err != nil {
		t.Fatalf("ensureUserConfig returned error: %v", err)
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Errorf("existing config file was modified: %q", data)
	}
}

func TestInstallCmd_EndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	modelSrc := filepath.Join(t.TempDir(), config.ModelFileName)
	if err := os.WriteFile(modelSrc, []byte("model-content"), 0644); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	modelDir := filepath.Join(home, "models")

	installBinDir = binDir
	installModelSrc = modelSrc
	installModelDir = modelDir
	defer func() {
		installBinDir = ""
		installModelSrc = ""
		installModelDir = ""
	}()

	if err := installCmd.RunE(installCmd, nil); err != nil {
		t.Fatalf("install returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(binDir, "kg")); err != nil {
		t.Errorf("binary not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modelDir, config.ModelFileName)); err != nil {
		t.Errorf("model not installed: %v", err)
	}
	cfg := filepath.Join(home, ".config", "graphed", "config.yaml")
	if _, err := os.Stat(cfg); err != nil {
		t.Errorf("user config not written: %v", err)
	}
}
