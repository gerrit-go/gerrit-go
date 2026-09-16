package gitsvc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gerrit-go/internal/store"
)

// setupConflictRepo builds a project whose master and a one-commit change both
// edit the same line, so rebasing the change conflicts. It returns the service
// and the change number.
func setupConflictRepo(t *testing.T) (*Service, int64) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	svc := New(filepath.Join(dir, "git"), db)

	acct := &store.Account{Username: "alice", FullName: "Alice", Email: "a@x"}
	if err := db.CreateAccount(acct); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := svc.CreateProject("demo", ""); err != nil {
		t.Fatalf("create project: %v", err)
	}

	work := filepath.Join(dir, "work")
	git := func(wd string, args ...string) string {
		t.Helper()
		out, err := svc.runGit(wd, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	git(dir, "clone", "--quiet", svc.RepoDir("demo"), work)
	git(work, "config", "user.email", "a@x")
	git(work, "config", "user.name", "Alice")

	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(work, rel), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("file.txt", "line1\nline2\nline3\n")
	git(work, "add", "file.txt")
	git(work, "commit", "--quiet", "-m", "init")
	git(work, "push", "--quiet", "origin", "HEAD:refs/heads/master")

	// Change commit editing line2.
	git(work, "checkout", "--quiet", "-b", "feature")
	write("file.txt", "line1\ntheirs\nline3\n")
	git(work, "commit", "--quiet", "-am", "feature change\n\nChange-Id: Iabc123")
	featureSHA := git(work, "rev-parse", "HEAD")
	git(work, "push", "--quiet", "origin", featureSHA+":refs/changes/01/1/1")

	// Advance master with a conflicting edit to line2.
	git(work, "checkout", "--quiet", "master")
	write("file.txt", "line1\nours\nline3\n")
	git(work, "commit", "--quiet", "-am", "master change")
	git(work, "push", "--quiet", "origin", "HEAD:refs/heads/master")

	c := &store.Change{Project: "demo", Branch: "master", ChangeID: "Iabc123", Subject: "feature change", OwnerID: acct.ID, CurrentPS: 1}
	if err := db.CreateChange(c); err != nil {
		t.Fatalf("create change: %v", err)
	}
	if err := db.CreatePatchSet(&store.PatchSet{ChangeNumber: c.Number, Number: 1, CommitSHA: featureSHA}); err != nil {
		t.Fatalf("create patch set: %v", err)
	}
	if err := db.SetCurrentPatchSet(c.Number, 1); err != nil {
		t.Fatalf("set current ps: %v", err)
	}
	return svc, c.Number
}

func TestRebaseConflictsAndResolve(t *testing.T) {
	svc, num := setupConflictRepo(t)

	files, err := svc.RebaseConflicts(num)
	if err != nil {
		t.Fatalf("RebaseConflicts: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(files))
	}
	cf := files[0]
	if cf.Path != "file.txt" {
		t.Errorf("path = %q, want file.txt", cf.Path)
	}
	if !strings.Contains(cf.Ours, "ours") {
		t.Errorf("ours missing destination content: %q", cf.Ours)
	}
	if !strings.Contains(cf.Theirs, "theirs") {
		t.Errorf("theirs missing change content: %q", cf.Theirs)
	}
	if !strings.Contains(cf.Conflict, "<<<<<<<") {
		t.Errorf("conflict markers missing: %q", cf.Conflict)
	}

	// A clean rebase is impossible here, so resolving must produce patch set 2.
	nc, err := svc.ResolveRebase(num, map[string]string{"file.txt": "line1\nresolved\nline3\n"})
	if err != nil {
		t.Fatalf("ResolveRebase: %v", err)
	}
	if nc.NewPatchSet != 2 {
		t.Errorf("NewPatchSet = %d, want 2", nc.NewPatchSet)
	}

	// Verify the pushed patch-set ref contains the resolved content.
	out, err := svc.runGit(svc.RepoDir("demo"), "show", "refs/changes/01/1/2:file.txt")
	if err != nil {
		t.Fatalf("show resolved file: %v", err)
	}
	if !strings.Contains(out, "resolved") {
		t.Errorf("resolved content not pushed: %q", out)
	}
}

func TestResolveRebaseMissingResolution(t *testing.T) {
	svc, num := setupConflictRepo(t)
	if _, err := svc.ResolveRebase(num, map[string]string{}); err == nil {
		t.Fatal("expected an error when a conflicting path has no resolution")
	}
}
