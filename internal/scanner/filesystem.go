package scanner

import (
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// File is the scanner's output for a single file.  It carries only
// the information downstream stages actually need (path, language,
// size, modification time) -- intentionally avoiding full fs.FileInfo
// to keep the pipeline lightweight and serialisable.
type File struct {
	Path      string
	Language  string
	Size      int64
	UpdatedAt time.Time
}

// FileSystemScanner walks the OS filesystem using filepath.WalkDir.
// It is the default scanner implementation; a memory-mapped or
// git-aware scanner could satisfy the same Scanner interface.
type FileSystemScanner struct {
	// Exclude carries extra gitignore-style patterns layered on top of any
	// discovered .gitignore files. They are matched relative to the scan root.
	Exclude []string
	// IgnoreFiles are paths to additional gitignore-format files, matched
	// relative to the scan root and layered after discovered .gitignore files.
	IgnoreFiles []string
	// NoGitIgnore disables discovery and application of .gitignore files.
	NoGitIgnore bool
	// NoDefaultSkip disables the built-in noise filter (VCS internals,
	// binary assets, lockfiles). When false -- the default -- such files are
	// excluded even if .gitignore does not mention them.
	NoDefaultSkip bool
}

// defaultSkipDirs are VCS/internal directories that are never graph content.
// They are pruned regardless of .gitignore: indexing a repo's own .git
// object store is pure noise and drags the corpus up by hundreds of files.
var defaultSkipDirs = map[string]bool{
	".git": true,
	".hg":  true,
	".svn": true,
}

// defaultSkipExtensions are binary/asset extensions that carry no semantic
// graph value: fonts, images, archives, media and compiled artifacts.
// Source-adjacent binaries with dedicated extractors (pdf, spreadsheet) are
// deliberately NOT listed so those formats stay indexable.
var defaultSkipExtensions = map[string]bool{
	// Images
	"png": true, "jpg": true, "jpeg": true, "gif": true, "svg": true,
	"webp": true, "ico": true, "bmp": true, "avif": true, "tiff": true,
	// Fonts
	"woff": true, "woff2": true, "ttf": true, "otf": true, "eot": true,
	// Archives
	"zip": true, "tar": true, "gz": true, "tgz": true, "bz2": true,
	"xz": true, "rar": true, "7z": true, "dmg": true, "pkg": true,
	// Media
	"mp4": true, "mov": true, "mkv": true, "avi": true, "webm": true,
	"mp3": true, "wav": true, "flac": true, "ogg": true, "aac": true,
	// Compiled artifacts & source maps
	"exe": true, "dll": true, "so": true, "dylib": true, "bin": true,
	"wasm": true, "class": true, "jar": true, "map": true,
}

// defaultSkipFilenames are boilerplate/OS artifacts that are not graph
// content: package lockfiles, vendored dependency metadata and filesystem
// junk that should never be parsed or embedded.
var defaultSkipFilenames = map[string]bool{
	"package-lock.json":   true,
	"yarn.lock":           true,
	"pnpm-lock.yaml":      true,
	"bun.lockb":           true,
	"bun.lock":            true,
	"go.sum":              true,
	"cargo.lock":          true,
	"gemfile.lock":        true,
	"composer.lock":       true,
	"poetry.lock":         true,
	"flake.lock":          true,
	"npm-shrinkwrap.json": true,
	".ds_store":           true,
	"desktop.ini":         true,
	"thumbs.db":           true,
}

// defaultSkip reports whether a directory or file should be dropped by the
// built-in noise filter. VCS dirs are matched by name; files by extension or
// well-known boilerplate filename.
func defaultSkip(path string, isDir bool) bool {
	if isDir {
		return defaultSkipDirs[strings.ToLower(filepath.Base(path))]
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if defaultSkipExtensions[ext] {
		return true
	}
	return defaultSkipFilenames[strings.ToLower(filepath.Base(path))]
}

// Scan walks the directory rooted at `root`, collecting every file
// and its detected language.  WalkDir is used instead of ReadDir
// because it short-circuits on errors and avoids loading entire
// directory listings into memory at once.
func (s *FileSystemScanner) Scan(root string) ([]File, error) {
	var err error
	var files []File
	var info fs.FileInfo
	var set *IgnoreSet

	set, err = loadGitIgnore(root, s.Exclude, s.IgnoreFiles, s.NoGitIgnore)
	if err != nil {
		return nil, err
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories that the ignore set matches, and prune the walk.
		if d.IsDir() {
			if !s.NoDefaultSkip && defaultSkip(path, true) {
				return filepath.SkipDir
			}
			if set != nil && set.excluded(path, true) {
				return filepath.SkipDir
			}
			return nil
		}

		// .gitignore files are configuration, not graph content.
		if d.Name() == ".gitignore" {
			return nil
		}

		// Get file details for the Size field
		info, err = d.Info()
		if err != nil {
			return err
		}

		// Append the clean, customized file structure directly into our slice
		if !s.NoDefaultSkip && defaultSkip(path, false) {
			return nil
		}
		if set == nil || !set.excluded(path, false) {
			files = append(files, File{
				Path:      path,
				Language:  detectLanguage(path),
				Size:      info.Size(),
				UpdatedAt: info.ModTime(),
			})
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, err
}

// detectLanguage maps a file name/extension to an internal language
// key that the parser uses to dispatch to the correct extractor.
// Returning a key (rather than a bool) lets the switch in Parse()
// grow cleanly as new languages are added.
func detectLanguage(path string) string {
	// Some formats are identified by filename rather than extension:
	// Dockerfiles and Makefiles ship without an extension by convention.
	if lang := detectLanguageByFilename(path); lang != "" {
		return lang
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "go":
		return "golang"
	case "pdf":
		return "pdf"
	case "md", "markdown":
		return "markdown"
	case "xlsx", "xls", "csv":
		return "spreadsheet"
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	case "toml":
		return "toml"
	case "js", "jsx", "mjs", "cjs":
		return "javascript"
	case "py", "pyi", "pyw":
		return "python"
	case "rs":
		return "rust"
	case "sql":
		return "sql"
	case "sh", "bash":
		return "bash"
	case "java":
		return "java"
	case "kt", "kts":
		return "kotlin"
	case "php":
		return "php"
	case "cs", "csx":
		return "csharp"
	case "swift":
		return "swift"
	case "rb", "gemspec":
		return "ruby"
	case "ex", "exs":
		return "elixir"
	case "c":
		return "c"
	case "h":
		return "c"
	case "cpp", "cc", "cxx", "hpp", "hh", "hxx":
		return "cpp"
	case "ts", "mts", "cts":
		return "typescript"
	case "tsx":
		return "tsx"
	case "dockerfile":
		return "dockerfile"
	case "mk", "make":
		return "make"
	default:
		return "unstructured" // fallback for plain text or unknown types
	}
}

// detectLanguageByFilename maps convention-based filenames to a
// language key. It returns "" when the name matches nothing, letting
// the caller fall through to extension-based detection.
func detectLanguageByFilename(path string) string {
	switch strings.ToLower(filepath.Base(path)) {
	case "dockerfile", "containerfile":
		return "dockerfile"
	case "makefile", "gnumakefile":
		return "make"
	case "gemfile", "rakefile", "podfile":
		return "ruby"
	case "mix.exs":
		return "elixir"
	}
	return ""
}
