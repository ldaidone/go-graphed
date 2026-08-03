// Package config centralises runtime configuration for the graphed pipeline.
//
// Values are resolved through an ordered chain of sources:
//
//	explicit overrides / CLI flags -> environment (GRAPHEAD_*) -> .env file
//	-> config file (JSON / YAML / TOML) -> defaults
//
// The config-file and .env sources are read with viper; nothing outside this
// package should read environment variables directly. Callers receive a fully
// resolved Settings value.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/viper"
)

// Well-known defaults and environment keys. Keeping them as exported
// constants lets the CLI wire flags (and future config-file readers)
// against the same names.
const (
	// ModelFileName is the embedding model looked up in the current
	// working directory when no path is configured explicitly.
	ModelFileName = "get-small.gtemodel"

	// DefaultDimensions is the embedding vector width of the bundled gte
	// small model (384 floats). It is only a reference value: runtime
	// configuration defaults Dimensions to 0, meaning "auto-detect from the
	// model", and 384 is used to document/validate the expected width.
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

	// Dimensions is the embedding vector width. 0 means "auto-detect from the
	// model"; any other value is validated against the model's actual width.
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
// defaults -> config file / .env -> environment -> Overrides.
func Load(o Overrides) (Settings, error) {
	var s Settings

	// 1. Build-time defaults. Dimensions defaults to 0 (auto-detect from the
	//    model); overrides and environment variables can set it explicitly.
	s.Dimensions = 0

	// 2. Config file + .env file sources, keyed by their GRAPHEAD_* variable
	//    names. The .env file already wins over the config file here.
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
		root = UserConfigDir()
		if root == "" {
			return "", fmt.Errorf("failed to get home directory")
		}
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

// UserConfigDir returns the user-level config directory used for the vector
// store, model installation, and the user config file (~/.config/graphed).
// It returns "" when the home directory cannot be determined.
func UserConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, DefaultDBRoot)
}

// loadFromFile merges the .env file (higher precedence) over the config file
// (JSON/YAML/TOML) and returns the resolved values keyed by their canonical
// GRAPHEAD_* environment variable names.
func loadFromFile() map[string]string {
	values := configFileValues()
	for k, v := range envFileValues() {
		values[k] = v
	}
	return values
}

// configFileValues reads the config file, discovered in precedence order:
// GRAPHEAD_CONFIG_FILE, then ./graphed.*, then ./.graphed.*, then
// ~/.config/graphed/config.*.
func configFileValues() map[string]string {
	path := discoverConfigPath()
	if path == "" {
		return map[string]string{}
	}

	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return map[string]string{}
	}
	return extractFileValues(v)
}

// discoverConfigPath returns the first existing config file, in precedence
// order: GRAPHEAD_CONFIG_FILE, then ./graphed.*, then ./.graphed.*, then
// ~/.config/graphed/config.*.
func discoverConfigPath() string {
	if explicit := os.Getenv(EnvConfigFile); explicit != "" {
		return explicit
	}
	for _, name := range []string{"graphed", ".graphed"} {
		if p := findConfigIn(".", name); p != "" {
			return p
		}
	}
	if dir := UserConfigDir(); dir != "" {
		return findConfigIn(dir, "config")
	}
	return ""
}

// findConfigIn looks for name.{json,yaml,yml,toml} inside dir.
func findConfigIn(dir, name string) string {
	for _, ext := range []string{".json", ".yaml", ".yml", ".toml"} {
		p := filepath.Join(dir, name+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// extractFileValues maps config-file keys onto their canonical environment
// variable names. Several key spellings are accepted because everything is
// keyed by the GRAPHEAD_* variable name during resolution.
func extractFileValues(v *viper.Viper) map[string]string {
	out := map[string]string{}
	if s := firstString(v, "model_path", "modelPath", "model-path"); s != "" {
		out[EnvModelPath] = s
	}
	if s := firstString(v, "db_root", "dbRoot", "db-root"); s != "" {
		out[EnvDBRoot] = s
	}
	if s := v.GetString("dimensions"); s != "" {
		out[EnvDimensions] = s
	}
	return out
}

// firstString returns the first non-empty value among keys.
func firstString(v *viper.Viper, keys ...string) string {
	for _, k := range keys {
		if s := v.GetString(k); s != "" {
			return s
		}
	}
	return ""
}

// envFileValues reads an optional .env file in the working directory. The
// keys are the plain GRAPHEAD_* variable names.
func envFileValues() map[string]string {
	const envFile = ".env"
	if _, err := os.Stat(envFile); err != nil {
		return map[string]string{}
	}

	v := viper.New()
	v.SetConfigFile(envFile)
	v.SetConfigType("env")
	if err := v.ReadInConfig(); err != nil {
		return map[string]string{}
	}

	out := map[string]string{}
	for _, key := range []string{EnvModelPath, EnvDBRoot, EnvDimensions} {
		if s := v.GetString(key); s != "" {
			out[key] = s
		}
	}
	return out
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
