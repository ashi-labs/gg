package e2e_test

import (
	"strings"
	"testing"
)

func TestInitEmptyDir(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	empty := e.dirUnder("empty")
	if _, err := e.gg(empty, "init"); err != nil {
		t.Fatal(err)
	}
	// Nested: container has .bare + main/ inside.
	if !e.exists(empty + "/.bare") {
		t.Error(".bare not created inside container")
	}
	if !e.exists(empty + "/main") {
		t.Error("primary worktree dir (main/) not created")
	}
	if !e.exists(empty + "/main/.git") {
		t.Error(".git gitfile missing in primary worktree")
	}
}

func TestInitDirWithFiles(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	dir := e.dirUnder("proj")
	e.writeFile(dir+"/notes.txt", "hi")
	if _, err := e.gg(dir, "init"); err != nil {
		t.Fatal(err)
	}
	// Files should be relocated into the primary worktree under <trunk>/.
	if e.exists(dir + "/notes.txt") {
		t.Error("notes.txt should have been moved into main/")
	}
	if !e.exists(dir + "/main/notes.txt") {
		t.Error("notes.txt missing from primary worktree")
	}
	if !e.exists(dir + "/.bare") {
		t.Error(".bare not created inside container")
	}
}

func TestInitAlreadyBareIsSilentNoOp(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	out, err := e.gg(e.upstream, "init")
	if err != nil {
		t.Fatalf("init on bare should succeed silently, actual: %v", err)
	}
	if out != "" {
		t.Errorf("bare init stdout should be empty, actual %q", out)
	}
}

func TestInitRefusesDirtyClone(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	regular := e.work + "/regular"
	mustExec(t, e.c, "", "git", "clone", e.upstream, regular)
	// Modify a tracked file (untracked alone wouldn't count post-loosening).
	e.writeFile(regular+"/README.md", "dirty content")
	_, err := e.gg(regular, "init")
	if err == nil {
		t.Fatal("init should refuse dirty clone")
	}
	if !strings.Contains(err.Error(), "uncommitted changes to tracked files") {
		t.Errorf("error should mention tracked-file changes, actual: %v", err)
	}
}

// Untracked-only files don't make the tree "dirty" for conversion — they
// just relocate alongside the tracked files. This guards against regressing
// to the stricter check that used to refuse untracked.
func TestInitConvertsCloneWithUntrackedFiles(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	regular := e.work + "/with-untracked"
	mustExec(t, e.c, "", "git", "clone", e.upstream, regular)
	e.writeFile(regular+"/scratch.txt", "scratch")
	if _, err := e.gg(regular, "init"); err != nil {
		t.Fatalf("untracked-only files should not block conversion, actual: %v", err)
	}
	if !e.exists(regular + "/main/scratch.txt") {
		t.Error("untracked file should have moved into the primary worktree")
	}
}

// When the user has a non-trunk branch checked out at the time of conversion,
// gg init should land them in a worktree for THAT branch (not trunk), keep
// their files in it, and register it as a tracked branch off trunk.
func TestInitConvertPreservesActiveBranch(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	regular := e.work + "/with-active"
	mustExec(t, e.c, "", "git", "clone", e.upstream, regular)
	mustExec(t, e.c, regular, "sh", "-c",
		"git checkout -b feat-active && echo feat > feat.txt && git add feat.txt && "+
			"git -c user.email=t@t -c user.name=t commit -m feat")
	e.writeFile(regular+"/scratch.txt", "scratch")

	primary, err := e.gg(regular, "init")
	if err != nil {
		t.Fatal(err)
	}
	actualWT := regular + "/feat-active"
	if primary != actualWT {
		t.Errorf("init stdout = %q, expected %q (active branch worktree)", primary, actualWT)
	}
	if !e.exists(regular + "/feat-active/feat.txt") {
		t.Error("feat.txt missing from active worktree")
	}
	if !e.exists(regular + "/feat-active/scratch.txt") {
		t.Error("untracked scratch.txt missing from active worktree")
	}
	if !e.exists(regular + "/main") {
		t.Error("trunk worktree not created")
	}
	if e.exists(regular + "/main/feat.txt") {
		t.Error("feat.txt should NOT appear in trunk worktree")
	}
	log, _ := e.gg(actualWT, "log")
	if !strings.Contains(log, "feat-active") {
		t.Errorf("gg log missing feat-active:\n%s", log)
	}
}

func TestInitConvertsRegularCloneToNested(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	regular := e.work + "/mycopy"
	mustExec(t, e.c, "", "git", "clone", e.upstream, regular)
	// Drop a tracked + an untracked file so we can verify both relocate.
	mustExec(
		t,
		e.c,
		regular,
		"sh",
		"-c",
		"echo extra > tracked.txt && git add tracked.txt && git -c user.email=t@t -c user.name=t commit -m extra",
	)
	e.writeFile(regular+"/untracked.txt", "u")

	if _, err := e.gg(regular, "init"); err != nil {
		t.Fatal(err)
	}

	// Bare lives inside the container.
	if !e.exists(regular + "/.bare") {
		t.Error(".bare missing inside container after nested conversion")
	}
	// Original .git should be gone (moved into .bare).
	if e.exists(regular + "/.git") {
		t.Error("top-level .git should be gone after conversion (moved into .bare)")
	}
	// Both the tracked and the untracked top-level files should now live
	// inside the primary worktree.
	if !e.exists(regular + "/main/tracked.txt") {
		t.Error("tracked file missing from primary worktree after conversion")
	}
	if !e.exists(regular + "/main/untracked.txt") {
		t.Error("untracked file missing from primary worktree after conversion")
	}
	if !e.exists(regular + "/main/.git") {
		t.Error(".git gitfile missing in primary worktree")
	}
	// Pre-existing top-level files should NOT linger at the container root.
	if e.exists(regular+"/tracked.txt") || e.exists(regular+"/untracked.txt") {
		t.Error("user files should have moved into main/, not stayed at container root")
	}
}
