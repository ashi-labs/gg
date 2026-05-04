package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDiffNoArgsShowsWorkingTree: a tracked-modified file should appear in the
// default `gg diff` (working tree vs HEAD).
func TestDiffNoArgsShowsWorkingTree(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "README.md"), "modified\n")
	out := e.ggMust(primary, "diff")
	if !strings.Contains(out, "README.md") || !strings.Contains(out, "+modified") {
		t.Errorf("default diff missing README.md change:\n%s", out)
	}
}

// TestDiffStagedShowsIndexOnly: after staging a change, `gg diff` is empty
// (working tree == index) but `gg diff --staged` shows it.
func TestDiffStagedShowsIndexOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "README.md"), "staged\n")
	mustExec(t, e.c, primary, "git", "add", "README.md")

	plain := e.ggMust(primary, "diff")
	if strings.Contains(plain, "+staged") {
		t.Errorf("plain diff should be empty after staging, got:\n%s", plain)
	}
	staged := e.ggMust(primary, "diff", "--staged")
	if !strings.Contains(staged, "+staged") {
		t.Errorf("--staged diff missing the staged change:\n%s", staged)
	}
}

// TestDiffParentShowsOnlyOwnCommits: on feat-b (child of feat-a), --parent
// should show feat-b's commits but not feat-a's. This is the "what does my
// branch add" view a reviewer sees.
func TestDiffParentShowsOnlyOwnCommits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "from-feat-a\n", "a-commit")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "from-feat-b\n", "b-commit")

	out := e.ggMust(fbPath, "diff", "--parent")
	if !strings.Contains(out, "b.txt") || !strings.Contains(out, "from-feat-b") {
		t.Errorf("--parent diff missing feat-b's own change:\n%s", out)
	}
	// feat-a's a.txt was committed before the feat-b branch was created,
	// so it's part of feat-b's parent — must NOT appear in --parent diff.
	if strings.Contains(out, "a.txt") {
		t.Errorf("--parent diff incorrectly includes feat-a's a.txt:\n%s", out)
	}
}

// TestDiffParentOnTrunkErrors: trunk has no parent; --parent must refuse.
func TestDiffParentOnTrunkErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	if _, err := e.gg(primary, "diff", "--parent"); err == nil {
		t.Fatal("expected --parent on trunk to error")
	}
}

// TestDiffBranchArg: `gg diff feat-a` from feat-b compares HEAD to feat-a,
// surfacing only feat-b's added commits.
func TestDiffBranchArg(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "from-feat-a\n", "a-commit")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "from-feat-b\n", "b-commit")

	out := e.ggMust(fbPath, "diff", "feat-a")
	if !strings.Contains(out, "b.txt") {
		t.Errorf("`diff feat-a` missing b.txt:\n%s", out)
	}
	if strings.Contains(out, "a.txt") {
		t.Errorf("`diff feat-a` should not include a.txt (already in feat-a):\n%s", out)
	}
}

// TestDiffPathScope: a positional path narrows the diff to that file only.
func TestDiffPathScope(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "README.md"), "modified\n")
	e.writeFile(filepath.Join(primary, "other.txt"), "other\n")
	mustExec(t, e.c, primary, "git", "add", "other.txt")
	// other.txt is staged-untracked, so plain diff (working tree vs index)
	// won't show it. Make it tracked-modified so it would show without a
	// scope, then verify the scope hides it.
	mustExec(t, e.c, primary, "git", "commit", "-m", "seed-other")
	e.writeFile(filepath.Join(primary, "other.txt"), "other-modified\n")

	out := e.ggMust(primary, "diff", "README.md")
	if !strings.Contains(out, "README.md") {
		t.Errorf("scoped diff missing README.md:\n%s", out)
	}
	if strings.Contains(out, "other.txt") {
		t.Errorf("scoped diff incorrectly includes other.txt:\n%s", out)
	}
}

// TestDiffStagedAndParentMutex: --staged and --parent describe different
// comparisons; combining them should error.
func TestDiffStagedAndParentMutex(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")

	if _, err := e.gg(faPath, "diff", "--staged", "--parent"); err == nil {
		t.Fatal("expected --staged --parent combo to error")
	}
}

// TestDiffBranchPlusFlagsErrors: a positional branch can't combine with
// --staged or --parent.
func TestDiffBranchPlusFlagsErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "b", "b-commit")

	if _, err := e.gg(fbPath, "diff", "--parent", "feat-a"); err == nil {
		t.Fatal("expected --parent + branch arg to error")
	}
	if _, err := e.gg(fbPath, "diff", "--staged", "feat-a"); err == nil {
		t.Fatal("expected --staged + branch arg to error")
	}
}

// TestDiffPathThatLooksLikeBranchNeedsDashDash: a positional that matches a
// branch name is treated as a branch by default; `--` forces path mode.
func TestDiffPathThatLooksLikeBranchNeedsDashDash(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	// Create a tracked branch named "config", and a tracked file at the
	// same name in the worktree, and modify the file.
	e.ggMust(primary, "append", "config")
	configWorktree := filepath.Join(e.work, "demo", "config")
	e.commitInto(configWorktree, "junk.txt", "x", "junk")
	// Back on trunk: create and modify a file literally named "config".
	e.writeFile(filepath.Join(primary, "config"), "v1\n")
	mustExec(t, e.c, primary, "git", "add", "config")
	mustExec(t, e.c, primary, "git", "commit", "-m", "add-config-file")
	e.writeFile(filepath.Join(primary, "config"), "v2\n")

	// Without --: "config" resolves as the branch name → diff between
	// trunk-HEAD and config-branch (which has the junk.txt commit). The
	// modified config FILE in the worktree shouldn't appear in this diff.
	branchMode := e.ggMust(primary, "diff", "config")
	if !strings.Contains(branchMode, "junk.txt") {
		t.Errorf("`diff config` should diff against the config BRANCH (junk.txt expected):\n%s", branchMode)
	}
	// With --: forces path mode → diff the modified config file in the
	// worktree.
	pathMode := e.ggMust(primary, "diff", "--", "config")
	if !strings.Contains(pathMode, "+v2") {
		t.Errorf("`diff -- config` should diff the FILE named config:\n%s", pathMode)
	}
}
