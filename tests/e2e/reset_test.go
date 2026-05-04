package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestResetUnstagesAll: with no flags or args, gg reset unstages
// everything (matches `git reset` aka --mixed HEAD).
func TestResetUnstagesAll(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "a.txt"), "a\n")
	e.writeFile(filepath.Join(primary, "b.txt"), "b\n")
	mustExec(t, e.c, primary, "git", "add", "a.txt", "b.txt")

	if _, err := e.gg(primary, "reset"); err != nil {
		t.Fatal(err)
	}
	status := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	for _, want := range []string{"?? a.txt", "?? b.txt"} {
		if !strings.Contains(status, want) {
			t.Errorf("after reset, expected %q in status:\n%s", want, status)
		}
	}
}

// TestResetUnstagesPath: a positional path scopes the unstage.
func TestResetUnstagesPath(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "a.txt"), "a\n")
	e.writeFile(filepath.Join(primary, "b.txt"), "b\n")
	mustExec(t, e.c, primary, "git", "add", "a.txt", "b.txt")

	if _, err := e.gg(primary, "reset", "a.txt"); err != nil {
		t.Fatal(err)
	}
	status := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	if !strings.Contains(status, "?? a.txt") {
		t.Errorf("a.txt should be unstaged:\n%s", status)
	}
	if !strings.Contains(status, "A  b.txt") {
		t.Errorf("b.txt should still be staged:\n%s", status)
	}
}

// TestResetSoftKeepsIndexAndWorktree: --soft moves HEAD back but leaves
// the index and working tree alone, so the rolled-back commit's content
// is now staged.
func TestResetSoftKeepsIndexAndWorktree(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a\n", "to-be-reset")

	if _, err := e.gg(faPath, "reset", "--soft", "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	status := mustExec(t, e.c, faPath, "git", "status", "--porcelain")
	if !strings.Contains(status, "A  a.txt") {
		t.Errorf("--soft should leave a.txt staged:\n%s", status)
	}
}

// TestResetMixedClearsIndex: --mixed moves HEAD back, clears the index,
// and leaves the working tree (the rolled-back content is now unstaged).
func TestResetMixedClearsIndex(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a\n", "to-be-reset")

	if _, err := e.gg(faPath, "reset", "--mixed", "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	status := mustExec(t, e.c, faPath, "git", "status", "--porcelain")
	if !strings.Contains(status, "?? a.txt") {
		t.Errorf("--mixed should leave a.txt untracked (index cleared, worktree kept):\n%s", status)
	}
}

// TestResetHardRequiresYesNoTTY: --hard refuses to run without --yes
// when there's no TTY for the confirmation prompt.
func TestResetHardRequiresYesNoTTY(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a\n", "to-be-reset")

	if _, err := e.gg(faPath, "reset", "--hard", "HEAD~1"); err == nil {
		t.Fatal("--hard without --yes on a non-TTY should error")
	}
}

// TestResetHardWipesWorktreeAndRestacksDescendants: --hard --yes drops
// the commit, removes its content from the working tree, AND restacks
// any descendant branches so they don't dangle off the gone tip.
func TestResetHardWipesWorktreeAndRestacksDescendants(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "to-be-reset\n", "feat-a tip")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "b\n", "feat-b commit")

	beforeFB := mustExec(t, e.c, fbPath, "git", "rev-parse", "HEAD")

	if _, err := e.gg(faPath, "reset", "--hard", "--yes", "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	if e.exists(filepath.Join(faPath, "a.txt")) {
		t.Errorf("--hard should have removed a.txt from the working tree")
	}
	afterFB := mustExec(t, e.c, fbPath, "git", "rev-parse", "HEAD")
	if beforeFB == afterFB {
		t.Errorf("feat-b HEAD unchanged — restack didn't fire after --hard reset")
	}
	// feat-b should still have its own b.txt content.
	bContents := mustExec(t, e.c, fbPath, "git", "show", "HEAD:b.txt")
	if bContents != "b" {
		t.Errorf("feat-b lost b.txt content after restack; got %q", bContents)
	}
}

// TestResetSoftDoesNotRestack: --soft moves HEAD but doesn't auto-restack.
// Descendants stay where they were (valid-but-out-of-sync), waiting for
// the user to run `gg restack` explicitly.
func TestResetSoftDoesNotRestack(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a\n", "feat-a tip")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "b\n", "feat-b commit")

	beforeFB := mustExec(t, e.c, fbPath, "git", "rev-parse", "HEAD")
	if _, err := e.gg(faPath, "reset", "--soft", "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	afterFB := mustExec(t, e.c, fbPath, "git", "rev-parse", "HEAD")
	if beforeFB != afterFB {
		t.Errorf("feat-b HEAD moved after --soft reset: %s -> %s (no auto-restack expected)", beforeFB, afterFB)
	}
}

// TestResetModesMutuallyExclusive: combining flags errors.
func TestResetModesMutuallyExclusive(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	if _, err := e.gg(primary, "reset", "--soft", "--hard"); err == nil {
		t.Fatal("expected --soft + --hard combo to error")
	}
}
