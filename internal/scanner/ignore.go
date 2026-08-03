package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// IgnoreRule binds a compiled gitignore matcher to the directory its
// patterns are relative to. The scanner may hold several of them: one per
// discovered .gitignore plus one for the user-supplied patterns.
type IgnoreRule struct {
	// dir is the directory that patterns are relative to. For a discovered
	// .gitignore this is the file's own directory; for user patterns it is
	// the scan root.
	dir string
	// matcher is the compiled pattern set. A nil matcher is skipped.
	matcher *ignore.GitIgnore
}

// IgnoreSet collects every ignore rule that applies under a scan root and
// answers "should this path be skipped?".
type IgnoreSet struct {
	// rules are ordered from highest precedence to lowest: user-supplied
	// patterns, user-provided ignore files, then discovered .gitignore files
	// from deepest to shallowest. A path's decision is taken from the first
	// rule that matches it, so deeper ignore files can re-include a path a
	// shallower one excluded, and user patterns always win.
	rules []IgnoreRule
}

// loadGitIgnore discovers .gitignore files under root (unless disabled) and
// layers the user-supplied patterns and ignore files on top. Returns nil when
// nothing applies.
func loadGitIgnore(root string, exclude []string, ignoreFiles []string, noGitIgnore bool) (*IgnoreSet, error) {
	set := &IgnoreSet{}

	// User-supplied patterns take precedence over every .gitignore file.
	if len(exclude) > 0 {
		set.rules = append(set.rules, IgnoreRule{
			dir:     root,
			matcher: ignore.CompileIgnoreLines(exclude...),
		})
	}

	// User-provided ignore files come next, matched relative to the scan root.
	for _, f := range ignoreFiles {
		if !filepath.IsAbs(f) {
			f = filepath.Join(root, f)
		}
		gi, err := ignore.CompileIgnoreFile(f)
		if err != nil {
			// A missing/malformed custom ignore file should not abort the scan.
			continue
		}
		set.rules = append(set.rules, IgnoreRule{
			dir:     root,
			matcher: gi,
		})
	}

	if !noGitIgnore {
		var files []string
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if d.Name() == ".gitignore" {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}

		// Sort deepest-first so more specific rules override shallower ones.
		sort.Slice(files, func(i, j int) bool {
			return pathDepth(files[i]) > pathDepth(files[j])
		})

		for _, f := range files {
			gi, err := ignore.CompileIgnoreFile(f)
			if err != nil {
				// A malformed .gitignore should not abort the whole scan.
				continue
			}
			set.rules = append(set.rules, IgnoreRule{
				dir:     filepath.Dir(f),
				matcher: gi,
			})
		}
	}

	return set, nil
}

// excluded reports whether path should be skipped. Paths are checked against
// every rule in precedence order: user-supplied patterns, user ignore files,
// then discovered .gitignore files from deepest to shallowest. The first rule
// that matches decides -- including a negation, which can re-include a path a
// lower-precedence rule excluded. This mirrors git's "the last matching
// pattern wins" rule given our ordering.
//
// isDir indicates the target is a directory. Directory checks append a
// trailing slash so dir-only patterns such as "build/" match the directory
// itself, letting the scanner prune excluded trees.
func (s *IgnoreSet) excluded(path string, isDir bool) bool {
	if s == nil {
		return false
	}

	for _, rule := range s.rules {
		rel, err := filepath.Rel(rule.dir, path)
		if err != nil {
			continue
		}
		// A rule only applies to paths beneath its own directory. Skip rules
		// from sibling/subdir .gitignore files that do not cover this path.
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if rule.matcher != nil {
			rel = filepath.ToSlash(rel)
			if isDir && !strings.HasSuffix(rel, "/") {
				rel += "/"
			}
			excluded, pattern := rule.matcher.MatchesPathHow(rel)
			// A non-nil pattern means the rule matched at least one pattern;
			// the bool carries the final decision for that file (a trailing
			// negation reverts it to false). A nil pattern means the rule did
			// not match at all, so fall through to the next rule up.
			if excluded || pattern != nil {
				return excluded
			}
		}
	}
	return false
}

// pathDepth counts the number of path separators, used to order rules.
func pathDepth(p string) int {
	return strings.Count(p, string(filepath.Separator))
}
