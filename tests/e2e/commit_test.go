package e2e_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCommitWithMessage: stage a file with raw `git add`, then `gg commit -m`
// should land it without opening an editor.
func TestCommitWithMessage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "new.txt"), "hi\n")
	mustExec(t, e.c, primary, "git", "add", "new.txt")

	if _, err := e.gg(primary, "commit", "-m", "first"); err != nil {
		t.Fatal(err)
	}
	subj := mustExec(t, e.c, primary, "git", "log", "-1", "--format=%s")
	if subj != "first" {
		t.Errorf("HEAD subject = %q, expected %q", subj, "first")
	}
}

// TestCommitAllFlagStagesTrackedOnly: -a should pick up tracked-modified files
// but leave untracked files alone (matches `git commit -a`).
func TestCommitAllFlagStagesTrackedOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Modify a tracked file (README.md was created by the seeded upstream).
	e.writeFile(filepath.Join(primary, "README.md"), "modified\n")
	// And add an untracked file that should NOT be picked up by -a.
	e.writeFile(filepath.Join(primary, "untracked.txt"), "no\n")

	if _, err := e.gg(primary, "commit", "-a", "-m", "trunk-edit"); err != nil {
		t.Fatal(err)
	}
	files := mustExec(t, e.c, primary, "git", "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(files, "README.md") {
		t.Errorf("HEAD missing README.md change:\n%s", files)
	}
	if strings.Contains(files, "untracked.txt") {
		t.Errorf("HEAD unexpectedly includes untracked.txt:\n%s", files)
	}
	// untracked.txt should still be sitting in the worktree, unstaged.
	status := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	if !strings.Contains(status, "?? untracked.txt") {
		t.Errorf("untracked.txt should still be untracked, status:\n%s", status)
	}
}

// TestCommitWithPathLimitsScope: stage two files, then commit only one by name;
// the other should remain staged for a follow-up commit.
func TestCommitWithPathLimitsScope(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "a.txt"), "a\n")
	e.writeFile(filepath.Join(primary, "b.txt"), "b\n")
	mustExec(t, e.c, primary, "git", "add", "a.txt", "b.txt")

	if _, err := e.gg(primary, "commit", "-m", "only-a", "a.txt"); err != nil {
		t.Fatal(err)
	}
	files := mustExec(t, e.c, primary, "git", "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(files, "a.txt") {
		t.Errorf("HEAD missing a.txt:\n%s", files)
	}
	if strings.Contains(files, "b.txt") {
		t.Errorf("HEAD unexpectedly includes b.txt:\n%s", files)
	}
	// b.txt should remain staged.
	status := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	if !strings.Contains(status, "A  b.txt") {
		t.Errorf("b.txt should still be staged, status:\n%s", status)
	}
}

// TestCommitDoesNotRestackDescendants pins the design decision: a plain commit
// on a non-tip branch must NOT touch descendants. The new commit appends
// (doesn't rewrite), so descendants stay valid-but-behind. They only catch up
// when the user explicitly runs `gg restack`.
func TestCommitDoesNotRestackDescendants(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(faPath, "append", "feat-a-1")
	a1Path := filepath.Join(e.work, "demo", "feat-a-1")
	e.commitInto(a1Path, "b.txt", "b", "b-commit")

	beforeA1 := mustExec(t, e.c, a1Path, "git", "rev-parse", "HEAD")

	// New commit on feat-a (mid-stack).
	e.writeFile(filepath.Join(faPath, "a2.txt"), "a2\n")
	mustExec(t, e.c, faPath, "git", "add", "a2.txt")
	if _, err := e.gg(faPath, "commit", "-m", "a-commit-2"); err != nil {
		t.Fatal(err)
	}

	afterA1 := mustExec(t, e.c, a1Path, "git", "rev-parse", "HEAD")
	if beforeA1 != afterA1 {
		t.Errorf("feat-a-1 HEAD moved after commit on feat-a (auto-restack happened?): %s → %s",
			beforeA1, afterA1)
	}
	a1Log := mustExec(t, e.c, a1Path, "git", "log", "--format=%s")
	if strings.Contains(a1Log, "a-commit-2") {
		t.Errorf("feat-a-1 unexpectedly contains a-commit-2 (auto-restack happened?):\n%s", a1Log)
	}
}

// TestCommitNoVerifyBypassesHook: install a pre-commit hook that always fails,
// confirm a normal commit fails, then confirm --no-verify lets it through.
func TestCommitNoVerifyBypassesHook(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hook script uses /bin/sh")
	}
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Resolve the per-worktree hooksPath. Worktrees share the bare's hooks dir
	// by default, so a hook installed there fires for every worktree's commits.
	hooksDir := mustExec(t, e.c, primary, "git", "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(primary, hooksDir)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(hooksDir, "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho rejected >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	e.writeFile(filepath.Join(primary, "x.txt"), "x\n")
	mustExec(t, e.c, primary, "git", "add", "x.txt")

	if _, err := e.gg(primary, "commit", "-m", "blocked"); err == nil {
		t.Fatal("commit should have been rejected by pre-commit hook")
	}
	if _, err := e.gg(primary, "commit", "--no-verify", "-m", "bypassed"); err != nil {
		t.Fatalf("--no-verify should bypass the hook: %v", err)
	}
	subj := mustExec(t, e.c, primary, "git", "log", "-1", "--format=%s")
	if subj != "bypassed" {
		t.Errorf("HEAD subject = %q, expected %q", subj, "bypassed")
	}
}
