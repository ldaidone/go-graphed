// Package git provides opt-in, pure-Go git awareness for kg builds.
//
// It is deliberately small: detect the enclosing repository, report working
// tree status per file, describe HEAD, and list recent commits with the files
// they touched so the builder can emit `co_changed` links. It never shells
// out to a git binary and has no effect unless the caller opts in via
// BuildOptions (GitAware / ChangedOnly).
package git

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
)

// CommitInfo is the subset of commit data kg needs: identity plus the
// repo-relative file paths the commit touched.
type CommitInfo struct {
	Hash    string
	Author  string
	Date    time.Time
	Message string
	Files   []string
}

// HeadInfo describes the current HEAD commit of a repository.
type HeadInfo struct {
	Hash   string
	Author string
	Date   time.Time
}

// maxFilesPerCommit caps how many touched files are recorded per commit so a
// single vendoring commit cannot explode the co-change pair space.
const maxFilesPerCommit = 50

// Open finds the enclosing git repository for path, following parent
// directories like the git CLI does. It returns the repository and the
// work-tree root (the directory containing .git).
func Open(path string) (*gogit.Repository, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	repo, err := gogit.PlainOpenWithOptions(abs, &gogit.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, "", err
	}
	root, err := repoRoot(abs)
	if err != nil {
		return nil, "", err
	}
	return repo, root, nil
}

// repoRoot walks up from path to the directory holding .git.
func repoRoot(path string) (string, error) {
	dir := path
	if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// Status returns working-tree status keyed by absolute file path. Values are
// go-git status codes ("M", "A", "D", "?", ...); unmodified tracked files are
// absent from the map. A nil map with nil error means clean.
func Status(repoRoot string) (map[string]string, error) {
	repo, root, err := Open(repoRoot)
	if err != nil {
		return nil, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	st, err := wt.Status()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(st))
	for rel, code := range st {
		// Unmodified entries are not reported by go-git status; keep the
		// guard so future versions stay cheap.
		if code.Worktree == gogit.Unmodified && code.Staging == gogit.Unmodified {
			continue
		}
		c := string(code.Worktree)
		if code.Staging != gogit.Unmodified {
			c = string(code.Staging)
		}
		if c == "?" || c == "" {
			c = "?"
		}
		out[filepath.Join(root, filepath.FromSlash(rel))] = c
	}
	return out, nil
}

// Head returns the current HEAD commit identity. Detached or unborn HEADs
// return an error so callers can degrade gracefully.
func Head(repoRoot string) (HeadInfo, error) {
	var hi HeadInfo
	repo, _, err := Open(repoRoot)
	if err != nil {
		return hi, err
	}
	ref, err := repo.Head()
	if err != nil {
		return hi, err
	}
	c, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return hi, err
	}
	hi.Hash = c.Hash.String()
	hi.Author = c.Author.Name
	hi.Date = c.Author.When
	return hi, nil
}

// Recent returns up to limit recent commits reachable from HEAD, newest
// first, each with the repo-relative paths it touched. limit <= 0 means a
// sane default of 50.
func Recent(repoRoot string, limit int) ([]CommitInfo, error) {
	if limit <= 0 {
		limit = 50
	}
	repo, _, err := Open(repoRoot)
	if err != nil {
		return nil, err
	}
	ref, err := repo.Head()
	if err != nil {
		return nil, err
	}
	iter, err := repo.Log(&gogit.LogOptions{From: ref.Hash(), Order: gogit.LogOrderCommitterTime})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []CommitInfo
	for len(out) < limit {
		c, err := iter.Next()
		if err != nil {
			break
		}
		ci := CommitInfo{
			Hash:    c.Hash.String(),
			Author:  c.Author.Name,
			Date:    c.Author.When,
			Message: firstLine(c.Message),
		}
		stats, serr := c.Stats()
		if serr == nil {
			for _, s := range stats {
				if len(ci.Files) >= maxFilesPerCommit {
					break
				}
				ci.Files = append(ci.Files, s.Name)
			}
		}
		out = append(out, ci)
	}
	return out, nil
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

// Fingerprint returns a short opaque string identifying the tree state at
// root: HEAD hash plus the sorted working-tree status. Two calls return
// different values iff something committed or changed between them, so MCP
// and the future `query` command can share it as a cheap freshness signal.
// Non-repos return an error for callers to degrade gracefully.
func Fingerprint(root string) (string, error) {
	head, err := Head(root)
	if err != nil {
		return "", err
	}
	status, err := Status(root)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(status)+1)
	lines = append(lines, "HEAD "+head.Hash)
	for path, code := range status {
		lines = append(lines, code+" "+path)
	}
	sort.Strings(lines[1:])
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])[:16], nil
}
