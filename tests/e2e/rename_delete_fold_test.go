package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRename(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.ggMust(faPath, "append", "feat-a-1")

	newPath := e.ggMust(faPath, "rename", "feat-x")
	if !strings.HasSuffix(newPath, "feat-x") {
		t.Errorf("renamed path = %q", newPath)
	}
	if e.exists(faPath) {
		t.Error("old worktree dir should be gone after rename")
	}
	if !e.exists(newPath) {
		t.Errorf("new worktree dir missing at %s", newPath)
	}
	// Child (feat-a-1) should now reference feat-x as parent.
	log, _ := e.gg(primary, "log")
	if !strings.Contains(log, "feat-x") || !strings.Contains(log, "feat-a-1") {
		t.Errorf("log missing renamed/child branches:\n%s", log)
	}
	// Nothing in the log should still mention the old feat-a name.
	if strings.Contains(log, "○ feat-a\n") {
		t.Errorf("log still shows old name:\n%s", log)
	}
}

func TestDeleteLeaf(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	fbPath := e.ggMust(primary, "new", "feat-b")

	if _, err := e.gg(primary, "delete", "--yes", "feat-b"); err != nil {
		t.Fatal(err)
	}
	if e.exists(fbPath) {
		t.Error("feat-b worktree should be removed")
	}
	log, _ := e.gg(primary, "log")
	if strings.Contains(log, "feat-b") {
		t.Errorf("feat-b still in log:\n%s", log)
	}
}

func TestDeleteReparentsChildren(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.ggMust(faPath, "append", "feat-a-1")

	// Delete feat-a from the primary worktree (not inside feat-a).
	if _, err := e.gg(primary, "delete", "--yes", "feat-a"); err != nil {
		t.Fatal(err)
	}
	// feat-a-1 should now appear directly under main.
	log, _ := e.gg(primary, "log")
	if strings.Contains(log, "feat-a\n") {
		t.Errorf("feat-a should be gone:\n%s", log)
	}
	if !strings.Contains(log, "feat-a-1") {
		t.Errorf("feat-a-1 should be reparented onto main:\n%s", log)
	}
}

func TestDeleteRefusesCurrent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	faPath := e.ggMust(primary, "append", "feat-a")
	if _, err := e.gg(faPath, "delete", "feat-a"); err == nil {
		t.Error("delete should refuse deleting the branch you're in")
	}
}

func TestDeleteRecursiveRemovesSubtree(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.ggMust(faPath, "append", "feat-a-1")
	a1Path := filepath.Join(e.work, "demo", "feat-a-1")
	e.ggMust(a1Path, "append", "feat-a-2")
	a2Path := filepath.Join(e.work, "demo", "feat-a-2")
	// Unrelated stack — should be untouched.
	fbPath := e.ggMust(primary, "new", "feat-b")

	// From primary (not inside the doomed subtree), wipe feat-a + downstream.
	if _, err := e.gg(primary, "rm", "-rb", "--yes", "feat-a"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{faPath, a1Path, a2Path} {
		if e.exists(p) {
			t.Errorf("worktree still exists: %s", p)
		}
	}
	// Sibling stack survives.
	if !e.exists(fbPath) {
		t.Errorf("sibling stack feat-b should survive")
	}
	log, _ := e.gg(primary, "log")
	for _, gone := range []string{"feat-a\n", "feat-a-1", "feat-a-2"} {
		if strings.Contains(log, gone) {
			t.Errorf("log still mentions %q after --recursive delete:\n%s", gone, log)
		}
	}
	if !strings.Contains(log, "feat-b") {
		t.Errorf("feat-b missing from log:\n%s", log)
	}
}

func TestDeleteRecursiveRefusesFromInside(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.ggMust(faPath, "append", "feat-a-1")
	a1Path := filepath.Join(e.work, "demo", "feat-a-1")

	// Sitting inside feat-a-1, try to delete feat-a's subtree. Refuses.
	_, err := e.gg(a1Path, "rm", "--recursive", "--branch", "feat-a")
	if err == nil {
		t.Fatal("delete --recursive from inside the subtree should refuse")
	}
	if !strings.Contains(err.Error(), "upstream") {
		t.Errorf("error should hint to `gg upstream`, actual: %v", err)
	}
}

func TestFoldSquashesAndRebasesChildren(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(faPath, "append", "feat-a-1")
	a1Path := filepath.Join(e.work, "demo", "feat-a-1")
	e.commitInto(a1Path, "b.txt", "b", "b-commit")

	// Fold feat-a-1 into feat-a.
	parentPath, err := e.gg(a1Path, "fold", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if parentPath != faPath {
		t.Errorf("fold returned %q, expected %q", parentPath, faPath)
	}
	if e.exists(a1Path) {
		t.Error("feat-a-1 worktree should be gone after fold")
	}
	// feat-a's HEAD should now include both commits (via squash).
	log, _ := e.gg(primary, "log")
	if strings.Contains(log, "feat-a-1") {
		t.Errorf("feat-a-1 should be gone from log:\n%s", log)
	}
}

func TestFoldWithChildren(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(faPath, "append", "feat-mid")
	midPath := filepath.Join(e.work, "demo", "feat-mid")
	e.commitInto(midPath, "mid.txt", "mid", "mid-commit")
	e.ggMust(midPath, "append", "feat-leaf")

	// Fold feat-mid into feat-a. feat-leaf should be rebased onto feat-a.
	if _, err := e.gg(midPath, "fold", "--yes"); err != nil {
		t.Fatal(err)
	}
	if e.exists(midPath) {
		t.Error("feat-mid worktree should be gone")
	}
	log, _ := e.gg(primary, "log")
	if strings.Contains(log, "feat-mid") {
		t.Errorf("feat-mid should be gone from log:\n%s", log)
	}
	if !strings.Contains(log, "feat-leaf") {
		t.Errorf("feat-leaf should survive (reparented onto feat-a):\n%s", log)
	}
}
