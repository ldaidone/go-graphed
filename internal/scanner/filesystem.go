package scanner

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// File is the scanner's output for a single file.  It carries only
// the information downstream stages actually need (path, language,
// size) -- intentionally avoiding full fs.FileInfo to keep the
// pipeline lightweight and serialisable.
type File struct {
	Path     string
	Language string
	Size     int64
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
		if set == nil || !set.excluded(path, false) {
			files = append(files, File{
				Path:     path,
				Language: detectLanguage(path),
				Size:     info.Size(),
			})
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, err
}

// detectLanguage maps a file extension to an internal language key
// that the parser uses to dispatch to the correct extractor.
// Returning a key (rather than a bool) lets the switch in Parse()
// grow cleanly as new languages are added.
func detectLanguage(path string) string {
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
	default:
		return "unstructured" // fallback for plain text or unknown types
	}
}
