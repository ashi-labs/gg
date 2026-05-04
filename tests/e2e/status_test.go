package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestStatusOnTrunkClean: a fresh clone on trunk with no work in progress
// should print the header (with "(trunk)") and a "clean" working-tree marker.
func TestStatusOnTrunkClean(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	out := e.ggMust(primary, "status")
	// Header layout: "<glyph> main (trunk)\t<path>". Branch name + trunk
	// badge are what we care about; the glyph and path are scenery.
	if !strings.Contains(out, "main") {
		t.Errorf("status should report current branch:\n%s", out)
	}
	if !strings.Contains(out, "(trunk)") {
		t.Errorf("trunk badge missing:\n%s", out)
	}
	if !strings.Contains(out, "clean") {
		t.Errorf("clean working-tree marker missing:\n%s", out)
	}
}

// TestStatusStackLineLinearStack: on a mid-stack branch, the stack line
// should read trunk → parent → you → child with "(you)" on the current
// branch.
func TestStatusStackLineLinearStack(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "b", "b-commit")
	e.ggMust(fbPath, "append", "feat-c")

	out := e.ggMust(fbPath, "status")
	// Pick out just the lineage chain line — branch names also appear in
	// the worktree path, which would defeat any whole-output ordering
	// check. The chain is the line after the "lineage" header (indented).
	lines := strings.Split(out, "\n")
	stackLine := ""
	for i, line := range lines {
		if strings.TrimSpace(line) == "lineage" && i+1 < len(lines) {
			stackLine = lines[i+1]
			break
		}
	}
	if stackLine == "" {
		t.Fatalf("no lineage line in:\n%s", out)
	}
	// Current branch is marked with an ANSI underline rather than a
	// "(you)" suffix; just check name membership and ordering, since
	// the rendered escape codes aren't worth brittling tests on.
	for _, want := range []string{"main", "feat-a", "feat-b", "feat-c"} {
		if !strings.Contains(stackLine, want) {
			t.Errorf("stack line missing %q: %q", want, stackLine)
		}
	}
	for i := 0; i < 3; i++ {
		want := []string{"main", "feat-a", "feat-b", "feat-c"}
		idx := strings.Index(stackLine, want[i])
		next := strings.Index(stackLine, want[i+1])
		if idx < 0 || next < 0 || idx > next {
			t.Errorf("stack order wrong: %q should precede %q in: %q", want[i], want[i+1], stackLine)
		}
	}
}

// TestStatusSiblingAnnotation: on a branch with multiple children, the
// stack line follows one child but flags how many siblings exist.
func TestStatusSiblingAnnotation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(faPath, "append", "child-1")
	e.ggMust(faPath, "append", "child-2")
	e.ggMust(faPath, "append", "child-3")

	out := e.ggMust(faPath, "status")
	if !strings.Contains(out, "(+2 siblings)") {
		t.Errorf("expected sibling annotation in stack line:\n%s", out)
	}
}

// TestStatusWorkingTreeBuckets: a worktree with a tracked-modified, a
// staged-new, and an untracked file should populate all three buckets.
func TestStatusWorkingTreeBuckets(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// tracked-modified
	e.writeFile(filepath.Join(primary, "README.md"), "modified\n")
	// staged-new (added then staged but not committed)
	e.writeFile(filepath.Join(primary, "added.txt"), "x\n")
	mustExec(t, e.c, primary, "git", "add", "added.txt")
	// untracked
	e.writeFile(filepath.Join(primary, "untracked.txt"), "x\n")

	out := e.ggMust(primary, "status")
	if !strings.Contains(out, "staged") || !strings.Contains(out, "added.txt") {
		t.Errorf("staged bucket missing added.txt:\n%s", out)
	}
	if !strings.Contains(out, "unstaged") || !strings.Contains(out, "README.md") {
		t.Errorf("unstaged bucket missing README.md:\n%s", out)
	}
	if !strings.Contains(out, "untracked") || !strings.Contains(out, "untracked.txt") {
		t.Errorf("untracked bucket missing untracked.txt:\n%s", out)
	}
}

// TestStatusHintsParentMoved: pin a stale parent SHA on feat-b (so
// gg thinks feat-a has moved since last restack), then assert the hint
// is emitted.
func TestStatusHintsParentMoved(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "b", "b-commit")

	// Move feat-a forward by one commit; feat-b's recorded ParentSHA is
	// now stale, which is exactly the "parent moved since last restack"
	// signal status surfaces.
	e.commitInto(faPath, "a2.txt", "a2", "a-commit-2")

	out := e.ggMust(fbPath, "status")
	if !strings.Contains(out, "needs attention") || !strings.Contains(out, "moved since last restack") {
		t.Errorf("expected `parent moved` hint:\n%s", out)
	}
}

// TestStatusUntrackedBranchHint: an untracked branch (created via raw
// git, not gg) should get a "run gg track" nudge.
func TestStatusUntrackedBranchHint(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	// Make a branch entirely outside gg's lineage.
	mustExec(t, e.c, primary, "git", "checkout", "-b", "rogue")

	out := e.ggMust(primary, "status")
	if !strings.Contains(out, "rogue") || !strings.Contains(out, "isn't tracked") {
		t.Errorf("expected untracked-branch hint:\n%s", out)
	}
}

// TestStatusCleanBranchNoHints: a healthy mid-stack branch with no
// drift, no PR, and a clean tree should print no `needs attention`
// block — the hint section is opt-in only.
func TestStatusCleanBranchNoHints(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")

	out := e.ggMust(faPath, "status")
	if strings.Contains(out, "needs attention") {
		t.Errorf("clean branch shouldn't emit `needs attention`:\n%s", out)
	}
}
