package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ldaidone/go-graphed/internal/config"
	"github.com/spf13/cobra"
)

// modelDownloadURL points at the bundled embedding model checked into the
// go-graphed repository root. kg install downloads it from here by default so
// the installed copy is always the complete, fresh model rather than whatever
// (possibly empty or stale) get-small.gtemodel happens to sit in the current
// directory. Kept as a variable so tests can point it at a local server.
var modelDownloadURL = "https://github.com/ldaidone/go-graphed/raw/main/get-small.gtemodel"

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

		modelDir := installModelDir
		if modelDir == "" {
			modelDir = filepath.Join(config.UserConfigDir(), "models")
		}

		// The model source resolves through three tiers:
		//   1. An explicit --model-path copies that local file verbatim.
		//   2. Otherwise download the bundled model straight from the
		//      go-graphed repository, so a stale/empty local file can never
		//      leave a broken model installed.
		//   3. If the download fails and a local model resolves (env, config,
		//      or ./get-small.gtemodel), fall back to copying it so offline
		//      installs still work.
		var modelPath string
		switch {
		case installModelSrc != "":
			modelPath, err = installModel(installModelSrc, modelDir)
			if err != nil {
				return err
			}
		default:
			modelPath, err = downloadModel(modelDir)
			if err != nil {
				if local := config.ResolveModelPath(""); local != "" {
					fmt.Printf("Warning: could not download model from %s (%v); falling back to local copy %s\n", modelDownloadURL, err, local)
					modelPath, err = installModel(local, modelDir)
					if err != nil {
						return err
					}
				} else {
					fmt.Printf("Warning: could not download model from %s: %v\n", modelDownloadURL, err)
					fmt.Println("Set GRAPHEAD_MODEL_PATH, place " + config.ModelFileName + " in the current directory, and run again to install it.")
				}
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
			fmt.Println("No embedding model installed; run install again from a network-connected environment or with a local model available.")
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

// downloadModel fetches the bundled embedding model from the go-graphed
// repository (modelDownloadURL) into modelDir as ModelFileName, returning the
// destination path. A non-2xx response or an empty body is treated as a
// failure so a truncated download can never masquerade as an installed model.
func downloadModel(modelDir string) (string, error) {
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create model directory %s: %w", modelDir, err)
	}
	dest := filepath.Join(modelDir, config.ModelFileName)

	resp, err := http.Get(modelDownloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download model from %s: %w", modelDownloadURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download model from %s: HTTP %s", modelDownloadURL, resp.Status)
	}

	var written int64
	if err := writeFileAtomic(dest, 0644, func(out io.Writer) error {
		written, err = io.Copy(out, resp.Body)
		if err != nil {
			return fmt.Errorf("failed to write %s: %w", dest, err)
		}
		return nil
	}); err != nil {
		return "", err
	}
	if written == 0 {
		return "", fmt.Errorf("downloaded model from %s is empty", modelDownloadURL)
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

	if err := writeFileAtomic(dest, mode, func(out io.Writer) error {
		if _, err := io.Copy(out, in); err != nil {
			return fmt.Errorf("failed to copy %s to %s: %w", src, dest, err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// writeFileAtomic writes to dest through a temporary file in the same
// directory that is renamed over dest, so the destination ends up with a fresh
// inode. Replacing the destination atomically (instead of truncating it in
// place) matters on macOS: if a running process has the file mapped as its
// executable — e.g. an active `kg mcp` server launched from the installed
// binary — truncating and rewriting it in place can make the kernel kill every
// subsequent exec of that file with SIGKILL until it is replaced by a new
// inode. Renaming a temp file over the destination leaves the running process
// on its old inode and hands new execs an intact file.
func writeFileAtomic(dest string, mode os.FileMode, write func(io.Writer) error) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dest)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file in %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("failed to replace %s: %w", dest, err)
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
		"Path to a local gte model to copy instead of downloading get-small.gtemodel from the repository",
	)

	installCmd.Flags().StringVar(
		&installModelDir,
		"model-dir",
		"",
		"Directory for the installed model (defaults to ~/.config/graphed/models)",
	)

	RootCmd.AddCommand(installCmd)
}
