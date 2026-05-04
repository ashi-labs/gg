package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRevertCreatesInverseCommit: revert produces a NEW commit that
// undoes the named one, leaving the original in history.
func TestRevertCreatesInverseCommit(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "to-revert\n", "add a.txt")

	if _, err := e.gg(faPath, "revert", "--no-edit", "HEAD"); err != nil {
		t.Fatal(err)
	}
	// The file added by the original commit should be gone now.
	if e.exists(filepath.Join(faPath, "a.txt")) {
		t.Errorf("a.txt should have been removed by the revert")
	}
	// History should contain BOTH the original commit and the revert.
	subjects := mustExec(t, e.c, faPath, "git", "log", "--format=%s", "main..HEAD")
	if !strings.Contains(subjects, "add a.txt") {
		t.Errorf("original commit should remain in history:\n%s", subjects)
	}
	if !strings.Contains(subjects, `Revert "add a.txt"`) {
		t.Errorf("revert commit subject missing:\n%s", subjects)
	}
}

// TestRevertDoesNotRestackDescendants: same contract as plain commit —
// revert appends a new commit, so descendants stay valid (just behind)
// and gg should NOT auto-restack.
func TestRevertDoesNotRestackDescendants(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a\n", "add a.txt")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "b\n", "add b.txt")

	beforeFB := mustExec(t, e.c, fbPath, "git", "rev-parse", "HEAD")

	if _, err := e.gg(faPath, "revert", "--no-edit", "HEAD"); err != nil {
		t.Fatal(err)
	}

	afterFB := mustExec(t, e.c, fbPath, "git", "rev-parse", "HEAD")
	if beforeFB != afterFB {
		t.Errorf("feat-b HEAD moved after revert on feat-a (auto-restack happened?): %s -> %s",
			beforeFB, afterFB)
	}
}

// TestRevertNoArgsErrors: revert requires at least one commit-ish.
func TestRevertNoArgsErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	if _, err := e.gg(primary, "revert"); err == nil {
		t.Fatal("expected `gg revert` with no args to error")
	}
}
