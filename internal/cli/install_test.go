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

func TestDefaultBinDir_FinalFallback(t *testing.T) {
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	t.Setenv("HOME", "")
	if got := defaultBinDir(); got != "." {
		t.Errorf("defaultBinDir() = %q, want current dir fallback", got)
	}
}

func TestInstallBinary_ErrorPaths(t *testing.T) {
	t.Run("dest directory is a file", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "bin")
		if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := installBinary("/does/not/exist", blocker); err == nil {
			t.Error("expected error when binDir is a file")
		}
	})

	t.Run("source does not exist", func(t *testing.T) {
		binDir := t.TempDir()
		_, err := installBinary(filepath.Join(binDir, "nope"), binDir)
		if err == nil {
			t.Error("expected error when source does not exist")
		}
	})
}

func TestInstallModel_ErrorPaths(t *testing.T) {
	t.Run("model directory is a file", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "models")
		if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := installModel("/does/not/exist", blocker); err == nil {
			t.Error("expected error when modelDir is a file")
		}
	})

	t.Run("source does not exist", func(t *testing.T) {
		modelDir := t.TempDir()
		_, err := installModel(filepath.Join(modelDir, "nope"), modelDir)
		if err == nil {
			t.Error("expected error when source does not exist")
		}
	})
}

func TestCopyFile_ErrorPaths(t *testing.T) {
	tests := []struct {
		name string
		src  func() string
		dest func() string
	}{
		{
			name: "missing source",
			src:  func() string { return filepath.Join(t.TempDir(), "missing") },
			dest: func() string { return filepath.Join(t.TempDir(), "out") },
		},
		{
			name: "dest directory missing",
			src: func() string {
				s := filepath.Join(t.TempDir(), "src")
				if err := os.WriteFile(s, []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
				return s
			},
			dest: func() string { return filepath.Join(t.TempDir(), "nested", "dir", "out") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := copyFile(tt.src(), tt.dest(), 0755); err == nil {
				t.Error("expected copyFile to return an error")
			}
		})
	}
}

func TestEnsureUserConfig_NoHomeDir(t *testing.T) {
	t.Setenv("HOME", "")
	if err := ensureUserConfig("/models/get-small.gtemodel"); err == nil {
		t.Error("expected error when the user config dir cannot be determined")
	}
}
