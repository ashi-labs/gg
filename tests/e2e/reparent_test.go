package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestReparentMovesSubtree: with no --pick, reparenting feat-b onto
// feat-x should drag feat-c (feat-b's child) along with it, and replay
// each branch's own commits on the new base.
func TestReparentMovesSubtree(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Stack 1: trunk → feat-a → feat-b → feat-c
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "b", "b-commit")
	e.ggMust(fbPath, "append", "feat-c")
	fcPath := filepath.Join(e.work, "demo", "feat-c")
	e.commitInto(fcPath, "c.txt", "c", "c-commit")

	// Stack 2 (sibling): trunk → feat-x
	e.ggMust(primary, "new", "feat-x")
	fxPath := filepath.Join(e.work, "demo", "feat-x")
	e.commitInto(fxPath, "x.txt", "x", "x-commit")

	// Move feat-b (and feat-c) onto feat-x.
	if _, err := e.gg(primary, "reparent", "feat-b", "feat-x"); err != nil {
		t.Fatal(err)
	}

	// feat-b's history: x-commit + b-commit. The a-commit must be gone
	// (we replayed feat-b's own commits on feat-x, dropping the path
	// through feat-a).
	bLog := mustExec(t, e.c, fbPath, "git", "log", "--format=%s")
	if !strings.Contains(bLog, "x-commit") {
		t.Errorf("feat-b should now contain x-commit:\n%s", bLog)
	}
	if !strings.Contains(bLog, "b-commit") {
		t.Errorf("feat-b should still contain b-commit:\n%s", bLog)
	}
	if strings.Contains(bLog, "a-commit") {
		t.Errorf("feat-b should NOT contain a-commit after move:\n%s", bLog)
	}

	// feat-c cascaded onto the new feat-b.
	cLog := mustExec(t, e.c, fcPath, "git", "log", "--format=%s")
	for _, want := range []string{"x-commit", "b-commit", "c-commit"} {
		if !strings.Contains(cLog, want) {
			t.Errorf("feat-c missing %q after cascade:\n%s", want, cLog)
		}
	}
	if strings.Contains(cLog, "a-commit") {
		t.Errorf("feat-c should NOT contain a-commit after move:\n%s", cLog)
	}

	// `gg up` from feat-c should land on feat-b (parent unchanged for the
	// child); `gg up` from feat-b should land on feat-x (the new parent).
	cUp, _ := e.gg(fcPath, "up")
	if cUp != fbPath {
		t.Errorf("feat-c up = %q, expected %q", cUp, fbPath)
	}
	bUp, _ := e.gg(fbPath, "up")
	if bUp != fxPath {
		t.Errorf("feat-b up = %q, expected %q (new parent)", bUp, fxPath)
	}
}

// TestReparentPickLeavesChildrenBehind: with --pick, reparenting feat-b
// onto trunk should move only feat-b. feat-c gets re-pointed to
// feat-a (feat-b's old parent) and its history loses the b-commit.
func TestReparentPickLeavesChildrenBehind(t *testing.T) {
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
	fcPath := filepath.Join(e.work, "demo", "feat-c")
	e.commitInto(fcPath, "c.txt", "c", "c-commit")

	// Pick-move feat-b out of the stack onto trunk.
	if _, err := e.gg(primary, "reparent", "--pick", "feat-b", "main"); err != nil {
		t.Fatal(err)
	}

	// feat-b: just b-commit on trunk; no a-commit.
	bLog := mustExec(t, e.c, fbPath, "git", "log", "--format=%s")
	if !strings.Contains(bLog, "b-commit") {
		t.Errorf("feat-b should still contain b-commit:\n%s", bLog)
	}
	if strings.Contains(bLog, "a-commit") {
		t.Errorf("feat-b should NOT contain a-commit after pick-move:\n%s", bLog)
	}

	// feat-c: rooted on feat-a, b-commit dropped from history.
	cLog := mustExec(t, e.c, fcPath, "git", "log", "--format=%s")
	if !strings.Contains(cLog, "a-commit") {
		t.Errorf("feat-c should still contain a-commit (parent is feat-a):\n%s", cLog)
	}
	if !strings.Contains(cLog, "c-commit") {
		t.Errorf("feat-c should still contain c-commit:\n%s", cLog)
	}
	if strings.Contains(cLog, "b-commit") {
		t.Errorf("feat-c should NOT contain b-commit after --pick:\n%s", cLog)
	}

	// feat-c's recorded parent is now feat-a (verify via `gg up`).
	cUp, _ := e.gg(fcPath, "up")
	if cUp != faPath {
		t.Errorf("feat-c up = %q, expected %q (feat-b's old parent)", cUp, faPath)
	}
	// feat-b's recorded parent is trunk.
	bUp, _ := e.gg(fbPath, "up")
	if bUp != primary {
		t.Errorf("feat-b up = %q, expected %q (trunk)", bUp, primary)
	}
}

// TestReparentOneArgUsesCurrentBranch: the 1-arg form treats the current
// branch as the source.
func TestReparentOneArgUsesCurrentBranch(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(primary, "new", "feat-x")
	fxPath := filepath.Join(e.work, "demo", "feat-x")
	e.commitInto(fxPath, "x.txt", "x", "x-commit")

	// From inside feat-a, reparent onto feat-x.
	if _, err := e.gg(faPath, "reparent", "feat-x"); err != nil {
		t.Fatal(err)
	}

	aLog := mustExec(t, e.c, faPath, "git", "log", "--format=%s")
	for _, want := range []string{"x-commit", "a-commit"} {
		if !strings.Contains(aLog, want) {
			t.Errorf("feat-a missing %q after 1-arg reparent:\n%s", want, aLog)
		}
	}
	aUp, _ := e.gg(faPath, "up")
	if aUp != fxPath {
		t.Errorf("feat-a up = %q, expected %q", aUp, fxPath)
	}
}

// TestReparentRefusesCycle: trying to reparent feat-a onto its own
// descendant must fail before any state changes — otherwise you get an
// unresolvable parent chain.
func TestReparentRefusesCycle(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.ggMust(faPath, "append", "feat-b")

	_, err := e.gg(primary, "reparent", "feat-a", "feat-b")
	if err == nil {
		t.Fatal("reparent should refuse: feat-b is a descendant of feat-a")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected cycle hint, actual: %v", err)
	}
}

// TestReparentRefusesTrunk: trunk has no parent — reparenting it is
// nonsense and must be refused early.
func TestReparentRefusesTrunk(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")

	_, err := e.gg(primary, "reparent", "main", "feat-a")
	if err == nil {
		t.Fatal("reparent should refuse trunk as the source")
	}
}

// TestReparentRefusesNoOp: moving onto the current parent is a no-op
// and should error rather than silently restacking.
func TestReparentRefusesNoOp(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.ggMust(faPath, "append", "feat-b")

	// feat-b's parent is already feat-a.
	_, err := e.gg(primary, "reparent", "feat-b", "feat-a")
	if err == nil {
		t.Fatal("reparent should refuse a no-op move")
	}
	if !strings.Contains(err.Error(), "already parented") {
		t.Errorf("expected 'already parented' hint, actual: %v", err)
	}
}

// TestReparentRefusesUnknownBranch: untracked branches can't participate
// on either side.
func TestReparentRefusesUnknownBranch(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")

	if _, err := e.gg(primary, "reparent", "ghost", "main"); err == nil {
		t.Error("reparent should refuse unknown source branch")
	}
	if _, err := e.gg(primary, "reparent", "feat-a", "ghost"); err == nil {
		t.Error("reparent should refuse unknown new-parent branch")
	}
}

// TestReparentOntoTrunk: pulling a branch out of a stack to be a direct
// child of trunk is a common shape — make sure the trunk identifier is
// accepted as the new parent.
func TestReparentOntoTrunk(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a", "a-commit")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "b", "b-commit")

	if _, err := e.gg(primary, "reparent", "feat-b", "main"); err != nil {
		t.Fatal(err)
	}
	bLog := mustExec(t, e.c, fbPath, "git", "log", "--format=%s")
	if !strings.Contains(bLog, "b-commit") {
		t.Errorf("feat-b should keep b-commit:\n%s", bLog)
	}
	if strings.Contains(bLog, "a-commit") {
		t.Errorf("feat-b should NOT contain a-commit after move to trunk:\n%s", bLog)
	}
	bUp, _ := e.gg(fbPath, "up")
	if bUp != primary {
		t.Errorf("feat-b up = %q, expected %q (trunk)", bUp, primary)
	}
}
