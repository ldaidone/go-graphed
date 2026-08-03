package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/spf13/cobra"
)

var (
	installBinDir   string
	installModelSrc string
	installModelDir string
)

// installCmd installs the kg binary and the bundled embedding model for
// global use: the binary goes into a bin directory on PATH, the model into
// the user config directory, and a user config file is written so future
// invocations pick up the installed model by default.
var installCmd = &cobra.Command{
	Use:     "install",
	Short:   "Install the kg binary and embedding model for global use",
	Args:    cobra.NoArgs,
	GroupID: "setup",
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		var exe string
		var binPath string

		exe, err = os.Executable()
		if err != nil {
			return fmt.Errorf("failed to locate the running binary: %w", err)
		}

		binDir := installBinDir
		if binDir == "" {
			binDir = defaultBinDir()
		}
		binPath, err = installBinary(exe, binDir)
		if err != nil {
			return err
		}

		modelSrc := installModelSrc
		if modelSrc == "" {
			modelSrc = config.ResolveModelPath("")
		}

		var modelPath string
		if modelSrc != "" {
			modelDir := installModelDir
			if modelDir == "" {
				modelDir = filepath.Join(config.UserConfigDir(), "models")
			}
			modelPath, err = installModel(modelSrc, modelDir)
			if err != nil {
				return err
			}
		}

		fmt.Printf("Installed binary: %s\n", binPath)
		if modelPath != "" {
			fmt.Printf("Installed model: %s\n", modelPath)
			if err := ensureUserConfig(modelPath); err != nil {
				return err
			}
			fmt.Printf("Wrote user config so future runs default to the installed model.\n")
		} else {
			fmt.Println("No embedding model found to install (set GRAPHEAD_MODEL_PATH or run from a directory containing " + config.ModelFileName + ").")
		}
		return nil
	},
}

// defaultBinDir returns the binary installation directory: GOBIN, then
// GOPATH/bin, then ~/.local/bin, then the current directory.
func defaultBinDir() string {
	if gb := os.Getenv("GOBIN"); gb != "" {
		return gb
	}
	if gp := os.Getenv("GOPATH"); gp != "" {
		return filepath.Join(gp, "bin")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "bin")
	}
	return "."
}

// installBinary copies src into binDir as "kg", making it executable.
func installBinary(src, binDir string) (string, error) {
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create bin directory %s: %w", binDir, err)
	}
	dest := filepath.Join(binDir, "kg")
	if err := copyFile(src, dest, 0755); err != nil {
		return "", err
	}
	return dest, nil
}

// installModel copies src into modelDir as ModelFileName.
func installModel(src, modelDir string) (string, error) {
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create model directory %s: %w", modelDir, err)
	}
	dest := filepath.Join(modelDir, config.ModelFileName)
	if err := copyFile(src, dest, 0644); err != nil {
		return "", err
	}
	return dest, nil
}

// copyFile copies src to dest, creating dest with the given mode.
func copyFile(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", dest, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", src, dest, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("failed to write %s: %w", dest, err)
	}
	return nil
}

// ensureUserConfig writes ~/.config/graphed/config.yaml pointing model_path
// at the freshly installed model, unless a config file already exists (an
// existing user config takes precedence and is left untouched).
func ensureUserConfig(modelPath string) error {
	dir := config.UserConfigDir()
	if dir == "" {
		return fmt.Errorf("could not determine user config directory")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}

	cfg := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(cfg); err == nil {
		fmt.Printf("Existing config file left untouched: %s\n", cfg)
		return nil
	}

	content := fmt.Sprintf("model_path: %s\n", modelPath)
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", cfg, err)
	}
	return nil
}

func init() {
	installCmd.Flags().StringVar(
		&installBinDir,
		"bin-dir",
		"",
		"Directory for the kg binary (defaults to GOBIN, GOPATH/bin, or ~/.local/bin)",
	)

	installCmd.Flags().StringVar(
		&installModelSrc,
		"model-path",
		"",
		"Path to the gte model to install (defaults to GRAPHEAD_MODEL_PATH or ./get-small.gtemodel)",
	)

	installCmd.Flags().StringVar(
		&installModelDir,
		"model-dir",
		"",
		"Directory for the installed model (defaults to ~/.config/graphed/models)",
	)

	RootCmd.AddCommand(installCmd)
}
