package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRmFile: default mode removes a tracked file from both the working
// tree and the index, leaving the deletion staged.
func TestRmFile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// README.md is tracked from the seed.
	if _, err := e.gg(primary, "rm", "README.md"); err != nil {
		t.Fatal(err)
	}
	if e.exists(filepath.Join(primary, "README.md")) {
		t.Errorf("README.md should be gone from the working tree")
	}
	status := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	if !strings.Contains(status, "D  README.md") {
		t.Errorf("expected staged deletion in status:\n%s", status)
	}
}

// TestRmCached: --cached drops the file from the index but leaves the
// working-tree copy.
func TestRmCached(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	if _, err := e.gg(primary, "rm", "--cached", "README.md"); err != nil {
		t.Fatal(err)
	}
	if !e.exists(filepath.Join(primary, "README.md")) {
		t.Errorf("--cached should keep the working-tree file")
	}
	status := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	if !strings.Contains(status, "D  README.md") {
		t.Errorf("expected staged deletion of README.md:\n%s", status)
	}
}

// TestRmBranch: -b delegates to the same code path as `gg delete`.
// Verifies the branch is gone from the lineage and the worktree is
// removed.
func TestRmBranch(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-doomed")
	doomedPath := filepath.Join(e.work, "demo", "feat-doomed")
	if !e.exists(doomedPath) {
		t.Fatal("setup: feat-doomed worktree missing")
	}

	if _, err := e.gg(primary, "rm", "-b", "-y", "feat-doomed"); err != nil {
		t.Fatal(err)
	}
	if e.exists(doomedPath) {
		t.Errorf("worktree should be gone after `gg rm -b feat-doomed`")
	}
	log := mustExec(t, e.c, primary, "gg", "log")
	if strings.Contains(log, "feat-doomed") {
		t.Errorf("feat-doomed should be gone from gg's lineage:\n%s", log)
	}
}

// TestRmBranchRequiresOneArg: `gg rm -b` with multiple branch names
// errors rather than guessing.
func TestRmBranchRequiresOneArg(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	if _, err := e.gg(primary, "rm", "-b", "feat-a", "feat-b"); err == nil {
		t.Fatal("`gg rm -b` should reject multiple positional args")
	}
}

// TestMvFile: default mode renames a tracked file via `git mv`.
func TestMvFile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	if _, err := e.gg(primary, "mv", "README.md", "DOC.md"); err != nil {
		t.Fatal(err)
	}
	if e.exists(filepath.Join(primary, "README.md")) {
		t.Errorf("README.md should have moved")
	}
	if !e.exists(filepath.Join(primary, "DOC.md")) {
		t.Errorf("DOC.md should now exist")
	}
}

// TestMvBranch: -b renames the current branch (delegates to gg rename).
func TestMvBranch(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-old")
	oldPath := filepath.Join(e.work, "demo", "feat-old")

	if _, err := e.gg(oldPath, "mv", "-b", "feat-new"); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(e.work, "demo", "feat-new")
	if !e.exists(newPath) {
		t.Errorf("renamed worktree should exist at %s", newPath)
	}
	if e.exists(oldPath) {
		t.Errorf("old worktree path should be gone")
	}
}

// TestMvBranchRequiresOneArg: `gg mv -b` takes exactly one positional
// (the new name); the source is always the current branch.
func TestMvBranchRequiresOneArg(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")

	if _, err := e.gg(faPath, "mv", "-b", "feat-a", "feat-b"); err == nil {
		t.Fatal("`gg mv -b` should reject more than one positional")
	}
}

// TestMvBranchOffersNoCompletions: in branch mode, mv takes a freeform
// new branch name; the source is always the current branch. Offering
// any candidate would be misleading (existing branches can't be the
// target — rename-to-existing fails — and there's nothing to complete
// for a name the user is inventing).
func TestMvBranchOffersNoCompletions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	e.ggMust(primary, "append", "feat-b")
	faPath := filepath.Join(e.work, "demo", "feat-a")

	out, err := e.gg(faPath, "__complete", "mv", "-b", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		t.Errorf("mv -b should offer no candidates, got: %q", line)
	}
}

// TestRmCompletionOffersTrackedNotUntracked: the file-mode completion
// for `gg rm` (and `gg mv`) should source from tracked files
// (committed/staged), not the dirty-files set. `git rm` flat-out
// refuses untracked files; suggesting them would be misleading.
func TestRmCompletionOffersTrackedNotUntracked(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Create one tracked file (committed) and one untracked file.
	e.writeFile(filepath.Join(primary, "tracked.txt"), "x\n")
	mustExec(t, e.c, primary, "git", "add", "tracked.txt")
	mustExec(t, e.c, primary, "git", "commit", "-m", "add tracked")
	e.writeFile(filepath.Join(primary, "untracked.txt"), "y\n")

	for _, cmdName := range []string{"rm", "mv"} {
		out, err := e.gg(primary, "__complete", cmdName, "")
		if err != nil {
			t.Fatalf("__complete %s: %v", cmdName, err)
		}
		var lines []string
		for _, line := range strings.Split(out, "\n") {
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			lines = append(lines, line)
		}
		foundTracked := false
		for _, name := range lines {
			if name == "tracked.txt" {
				foundTracked = true
			}
			if name == "untracked.txt" {
				t.Errorf("%s completion offered untracked.txt: %v", cmdName, lines)
			}
		}
		if !foundTracked {
			t.Errorf("%s completion missing tracked.txt: %v", cmdName, lines)
		}
	}
}

// TestRmBranchCompletionExcludesCurrent: same contract for `gg rm -b`
// — branches excluding the current one, since you can't delete the
// branch you're standing in.
func TestRmBranchCompletionExcludesCurrent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	e.ggMust(primary, "append", "feat-b")
	faPath := filepath.Join(e.work, "demo", "feat-a")

	out, err := e.gg(faPath, "__complete", "rm", "-b", "")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		name := strings.SplitN(line, "\t", 2)[0]
		lines = append(lines, name)
	}
	for _, name := range lines {
		if name == "feat-a" {
			t.Errorf("rm -b completion should exclude the current branch (feat-a):\n%v", lines)
		}
	}
	foundFeatB := false
	for _, name := range lines {
		if name == "feat-b" {
			foundFeatB = true
		}
	}
	if !foundFeatB {
		t.Errorf("rm -b completion missing feat-b: %v", lines)
	}
}

// TestRmMvAliasesGone: ensure the old `rm`/`mv` aliases on
// delete/rename are removed — they would silently shadow the new
// file-mode commands. `gg rm <branch>` (no -b) should now operate on
// a path, not the branch.
func TestRmAliasNoLongerDeletesBranch(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	if !e.exists(faPath) {
		t.Fatal("setup: feat-a worktree missing")
	}

	// `gg rm feat-a` (no -b) should NOT delete the branch — it should
	// try to remove a file named "feat-a" and fail because no such
	// path is tracked.
	if _, err := e.gg(primary, "rm", "feat-a"); err == nil {
		t.Errorf("`gg rm feat-a` (no -b) should fail (no such file), not delete the branch")
	}
	if !e.exists(faPath) {
		t.Errorf("feat-a worktree should still exist — `gg rm` (no -b) must not touch branches")
	}
}
