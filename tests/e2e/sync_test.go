package e2e_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyncFastForwardsTrunk: a collaborator pushes to origin; `gg sync` should
// pull it into the primary worktree.
func TestSyncFastForwardsTrunk(t *testing.T) {
	t.Parallel()
	e := newEnv(t).withOwnUpstream()
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Simulate another developer pushing a commit to origin/main.
	scratch := e.work + "/collab"
	mustExec(t, e.c, "", "git", "clone", e.upstream, scratch)
	e.commitInto(scratch, "remote.txt", "hello", "remote commit")
	mustExec(t, e.c, scratch, "git", "push", "origin", "main")

	if _, err := e.gg(primary, "sync"); err != nil {
		t.Fatal(err)
	}

	out := mustExec(t, e.c, primary, "git", "log", "--format=%s")
	if !strings.Contains(out, "remote commit") {
		t.Errorf("primary trunk missing remote commit:\n%s", out)
	}
}

// TestRestackCascades: advance trunk locally, then `gg restack` rebases
// feat-a and feat-a-1 onto the new trunk head.
func TestRestackCascades(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(faPath, "append", "feat-a-1")
	a1Path := filepath.Join(e.work, "demo", "feat-a-1")
	e.commitInto(a1Path, "b.txt", "b", "b-commit")

	// Advance trunk locally (simulating post-fetch state).
	e.commitInto(primary, "trunk.txt", "trunk", "trunk-commit")

	if _, err := e.gg(primary, "restack"); err != nil {
		t.Fatal(err)
	}

	// feat-a should have trunk-commit in its history.
	faLog := mustExec(t, e.c, faPath, "git", "log", "--format=%s")
	for _, expected := range []string{"trunk-commit", "a-commit"} {
		if !strings.Contains(faLog, expected) {
			t.Errorf("feat-a log missing %q:\n%s", expected, faLog)
		}
	}
	// feat-a-1 should also contain trunk-commit (cascaded).
	a1Log := mustExec(t, e.c, a1Path, "git", "log", "--format=%s")
	for _, expected := range []string{"trunk-commit", "a-commit", "b-commit"} {
		if !strings.Contains(a1Log, expected) {
			t.Errorf("feat-a-1 log missing %q:\n%s", expected, a1Log)
		}
	}
}

// TestRestackNoOpWhenNothingMoved: restack with trunk unchanged should not
// produce spurious rebases or errors.
func TestRestackNoOpWhenNothingMoved(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")

	// Record feat-a's current tip.
	before := mustExec(t, e.c, faPath, "git", "rev-parse", "HEAD")
	if _, err := e.gg(primary, "restack"); err != nil {
		t.Fatal(err)
	}
	after := mustExec(t, e.c, faPath, "git", "rev-parse", "HEAD")
	if before != after {
		t.Errorf("feat-a tip changed unexpectedly: %s → %s", before, after)
	}
}

// TestAbortRollsBackOnConflict: conflict during restack, then abort resets
// branches to their pre-sync SHAs.
func TestAbortRollsBackOnConflict(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	// feat-a modifies README.md to "from-feat-a".
	e.commitInto(faPath, "README.md", "from-feat-a", "feat-a edit")

	// Trunk modifies README.md to "from-trunk" (conflicting line).
	e.commitInto(primary, "README.md", "from-trunk", "trunk edit")

	preTrunk := mustExec(t, e.c, primary, "git", "rev-parse", "HEAD")
	preFeatA := mustExec(t, e.c, faPath, "git", "rev-parse", "HEAD")

	// Restack should conflict.
	_, err := e.gg(primary, "restack")
	if err == nil {
		t.Fatal("restack should have hit a conflict")
	}
	if !strings.Contains(err.Error(), "paused") {
		t.Errorf("expected pause hint, actual: %v", err)
	}

	// Abort.
	if _, err := e.gg(primary, "abort"); err != nil {
		t.Fatalf("abort: %v", err)
	}

	// Branches restored.
	if actual := mustExec(t, e.c, primary, "git", "rev-parse", "HEAD"); actual != preTrunk {
		t.Errorf("trunk not restored: %s vs %s", actual, preTrunk)
	}
	if actual := mustExec(t, e.c, faPath, "git", "rev-parse", "HEAD"); actual != preFeatA {
		t.Errorf("feat-a not restored: %s vs %s", actual, preFeatA)
	}
}

// TestContinueCompletesAfterManualResolve: same conflict as Abort, but user
// resolves it and runs `gg continue`.
func TestContinueCompletesAfterManualResolve(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "README.md", "from-feat-a", "feat-a edit")
	e.commitInto(primary, "README.md", "from-trunk", "trunk edit")

	_, err := e.gg(primary, "restack")
	if err == nil {
		t.Fatal("restack should have hit a conflict")
	}

	// Manually resolve: keep feat-a's version, then `git rebase --continue`.
	e.writeFile(faPath+"/README.md", "from-feat-a")
	mustExec(t, e.c, faPath, "git", "add", "README.md")
	// `git rebase --continue` needs GIT_EDITOR since the commit would normally
	// prompt for a message. Skip the editor.
	if _, _, err := e.c.exec(
		context.Background(),
		faPath,
		"sh",
		"-c",
		"GIT_EDITOR=true git rebase --continue",
	); err != nil {
		t.Fatal(err)
	}

	if _, err := e.gg(primary, "continue"); err != nil {
		t.Fatal(err)
	}

	// Rebase is done, runstate should be clear — a second restack should be
	// a clean no-op (except noting we just rebased, parent-sha matches).
	if _, err := e.gg(primary, "restack"); err != nil {
		t.Errorf("post-continue restack should be clean, actual: %v", err)
	}
}

// TestSyncIgnoresDirtySiblingStacks pins the fix for a real bug:
// a stack-scoped sync used to refuse when ANY tracked branch in the
// repo had a dirty worktree, even if that branch was in an unrelated
// stack. After the fix, only the in-scope branches' worktrees are
// checked for cleanliness — siblings can be dirty without blocking.
//
// Setup: two independent stacks off trunk (feat-a and feat-x). Dirty
// feat-x's worktree, then run `gg sync` from feat-a. It should succeed.
func TestSyncIgnoresDirtySiblingStacks(t *testing.T) {
	t.Parallel()
	e := newEnv(t).withOwnUpstream()
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Stack 1: feat-a (off trunk).
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")

	// Stack 2: feat-x (also off trunk; sibling, not in feat-a's stack).
	e.ggMust(faPath, "new", "feat-x")
	fxPath := filepath.Join(e.work, "demo", "feat-x")
	e.commitInto(fxPath, "x.txt", "x", "x-commit")

	// Dirty feat-x. Stack-scoped sync from feat-a must NOT care.
	e.writeFile(filepath.Join(fxPath, "x.txt"), "dirty edit\n")

	if _, err := e.gg(faPath, "sync"); err != nil {
		t.Fatalf("sync should ignore dirty sibling stack feat-x; got: %v", err)
	}
}

// TestSyncRefusesDirtyInScopeBranch is the inverse: if a branch
// IN-scope is dirty, sync must still refuse — that's the legitimate
// safety check the bug fix preserved.
func TestSyncRefusesDirtyInScopeBranch(t *testing.T) {
	t.Parallel()
	e := newEnv(t).withOwnUpstream()
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "b", "b-commit")

	// Advance trunk so the rebase plan is non-empty (otherwise the engine
	// short-circuits before the cleanliness check fires).
	scratch := e.work + "/collab"
	mustExec(t, e.c, "", "git", "clone", e.upstream, scratch)
	e.commitInto(scratch, "remote.txt", "hi", "remote commit")
	mustExec(t, e.c, scratch, "git", "push", "origin", "main")

	// feat-b is downstream of feat-a, so it's in feat-a's stack.
	e.writeFile(filepath.Join(fbPath, "b.txt"), "dirty edit\n")

	if _, err := e.gg(faPath, "sync"); err == nil {
		t.Fatal("sync should refuse when an in-scope branch (feat-b) has a dirty worktree")
	}
}
