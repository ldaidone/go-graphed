// Package config centralises runtime configuration for the graphed pipeline.
//
// Values are resolved through an ordered chain of sources so that adding a
// config file (.env or similar) later is purely additive:
//
//	defaults -> config file (future) -> environment variables -> explicit overrides
//
// Nothing outside this package should read environment variables directly;
// callers receive a fully resolved Settings value.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Well-known defaults and environment keys. Keeping them as exported
// constants lets the CLI wire flags (and future config-file readers)
// against the same names.
const (
	// ModelFileName is the embedding model looked up in the current
	// working directory when no path is configured explicitly.
	ModelFileName = "get-small.gtemodel"

	// DefaultDimensions is the embedding vector width assumed when no
	// value is provided (e.g. the gte small model outputs 384 floats).
	DefaultDimensions = 384

	// DefaultDBRoot is the config directory, relative to the user's home,
	// under which per-project BadgerDB stores are created.
	DefaultDBRoot = ".config/graphed"

	// Environment variable names.
	EnvModelPath  = "GRAPHEAD_MODEL_PATH"
	EnvDBRoot     = "GRAPHEAD_DB_ROOT"
	EnvDimensions = "GRAPHEAD_DIMENSIONS"
	EnvConfigFile = "GRAPHEAD_CONFIG_FILE"
)

// Settings is the fully resolved runtime configuration. A zero/empty value
// for ModelPath means semantic embeddings are disabled for that invocation.
type Settings struct {
	// ModelPath is the resolved path to a gte model file, or "" when disabled.
	ModelPath string

	// Dimensions is the embedding vector width (defaults to DefaultDimensions).
	Dimensions int

	// DBRoot is the base config directory for BadgerDB stores. When empty,
	// callers fall back to the default under the user's home directory.
	DBRoot string
}

// Overrides carries values injected by the CLI (flags) that take precedence
// over every other source. Zero values mean "not provided".
type Overrides struct {
	ModelPath  string
	Dimensions int
	DBRoot     string
}

// Load resolves Settings from the ordered source chain:
// defaults -> config file (future) -> environment -> Overrides.
func Load(o Overrides) (Settings, error) {
	var s Settings

	// 1. Build-time defaults.
	s.Dimensions = DefaultDimensions

	// 2. Future config file (.env / similar) source. No-op for now; the
	//    merge is written so a real reader only needs to be added here.
	fileValues := loadFromFile()

	// 3. Environment variables.
	envValues := map[string]string{
		EnvModelPath:  os.Getenv(EnvModelPath),
		EnvDBRoot:     os.Getenv(EnvDBRoot),
		EnvDimensions: os.Getenv(EnvDimensions),
	}

	// Dimensions: apply sources in ascending precedence.
	if v := fileValues[EnvDimensions]; v != "" {
		s.Dimensions = parseDimensions(v, s.Dimensions)
	}
	if v := envValues[EnvDimensions]; v != "" {
		s.Dimensions = parseDimensions(v, s.Dimensions)
	}
	if o.Dimensions > 0 {
		s.Dimensions = o.Dimensions
	}

	// ModelPath: resolution (explicit flag -> env -> cwd fallback) happens
	// in ResolveModelPath, which itself consults the config-file source.
	s.ModelPath = ResolveModelPath(o.ModelPath)

	// DBRoot: explicit override -> env -> config file -> default (resolved
	// relative to the home directory only when actually needed).
	switch {
	case o.DBRoot != "":
		s.DBRoot = o.DBRoot
	case envValues[EnvDBRoot] != "":
		s.DBRoot = envValues[EnvDBRoot]
	case fileValues[EnvDBRoot] != "":
		s.DBRoot = fileValues[EnvDBRoot]
	default:
		s.DBRoot = ""
	}

	return s, nil
}

// ResolveModelPath returns the embedding model path to use, in precedence order:
//
//	explicit (CLI flag) -> GRAPHEAD_MODEL_PATH -> config file -> ./get-small.gtemodel -> "" (disabled)
//
// Returning "" signals the caller that embeddings are disabled for this run.
func ResolveModelPath(explicit string) string {
	if explicit != "" {
		return explicit
	}

	if p := os.Getenv(EnvModelPath); p != "" {
		return p
	}

	if p := loadFromFile()[EnvModelPath]; p != "" {
		return p
	}

	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, ModelFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

// BadgerDBPath resolves the directory for a per-project BadgerDB store.
// When root is empty it defaults to ~/.config/graphed. The directory is
// created if it does not exist.
func BadgerDBPath(root string) (string, error) {
	if root == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		root = filepath.Join(homeDir, DefaultDBRoot)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	dbPath := filepath.Join(root, filepath.Base(cwd))
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create db directory: %w", err)
	}

	return dbPath, nil
}

// loadFromFile is the seam for a future config-file source (.env or similar).
// It currently returns no values; wiring a reader that parses a key=value
// file (discovered via GRAPHEAD_CONFIG_FILE or a default location) here is
// additive and requires no changes anywhere else.
func loadFromFile() map[string]string {
	return nil
}

// parseDimensions converts a string to an int, falling back to the default
// when the value is missing or malformed.
func parseDimensions(v string, fallback int) int {
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	if n <= 0 {
		return fallback
	}
	return n
}
