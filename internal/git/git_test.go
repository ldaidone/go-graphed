package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\n")
	write("b.go", "package b\n")
	if _, err := wt.Add("a.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("b.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("first", &gogit.CommitOptions{Author: &object.Signature{Name: "T", Email: "t@x", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	// Modify one file + leave one untracked.
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n// edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	write("c.go", "package c\n")
	return dir
}

func TestStatus_SeesModifiedAndUntracked(t *testing.T) {
	dir := initRepo(t)
	st, err := Status(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st[filepath.Join(dir, "a.go")]; !ok {
		t.Error("modified a.go missing from status")
	}
	if _, ok := st[filepath.Join(dir, "c.go")]; !ok {
		t.Error("untracked c.go missing from status")
	}
	if _, ok := st[filepath.Join(dir, "b.go")]; ok {
		t.Error("clean b.go should not appear in status")
	}
}

func TestHeadAndRecent(t *testing.T) {
	dir := initRepo(t)
	hi, err := Head(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hi.Hash == "" || hi.Author != "T" {
		t.Errorf("unexpected HEAD %+v", hi)
	}
	commits, err := Recent(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(commits))
	}
	if len(commits[0].Files) != 2 {
		t.Errorf("commit files = %v, want a.go+b.go", commits[0].Files)
	}
}

func TestNotARepo_Errors(t *testing.T) {
	if _, err := Status(t.TempDir()); err == nil {
		t.Error("Status outside a repo should error")
	}
	if _, err := Head(t.TempDir()); err == nil {
		t.Error("Head outside a repo should error")
	}
}

func TestFingerprint_ChangesOnEditAndCommit(t *testing.T) {
	dir := initRepo(t)
	before, err := Fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before == "" {
		t.Fatal("empty fingerprint")
	}
	// Working-tree edit changes the fingerprint.
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n// edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := Fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Error("fingerprint did not change after edit")
	}
	// Non-repo errors for graceful degradation.
	if _, err := Fingerprint(t.TempDir()); err == nil {
		t.Error("Fingerprint outside a repo should error")
	}
}
