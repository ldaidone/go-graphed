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
	if s.Dimensions != DefaultDimensions {
		t.Errorf("Dimensions = %d, want default %d", s.Dimensions, DefaultDimensions)
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
	if s.Dimensions != DefaultDimensions {
		t.Errorf("Dimensions = %d, want default %d", s.Dimensions, DefaultDimensions)
	}
}
