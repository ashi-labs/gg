package e2e_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestAmendNoEdit: --no-edit folds staged changes into HEAD without changing
// the subject. The tree should now include the new content; the message stays.
func TestAmendNoEdit(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "v1\n", "feat-a: v1")

	e.writeFile(filepath.Join(faPath, "a.txt"), "v2\n")
	mustExec(t, e.c, faPath, "git", "add", "a.txt")
	if _, err := e.gg(faPath, "amend", "--no-edit"); err != nil {
		t.Fatal(err)
	}

	subj := mustExec(t, e.c, faPath, "git", "log", "-1", "--format=%s")
	if subj != "feat-a: v1" {
		t.Errorf("subject changed unexpectedly: %q", subj)
	}
	contents := mustExec(t, e.c, faPath, "git", "show", "HEAD:a.txt")
	if contents != "v2" {
		t.Errorf("amended tree missing new content; got %q", contents)
	}
}

// TestAmendMessage: -m replaces the commit message in place.
func TestAmendMessage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "v1\n", "old subject")

	if _, err := e.gg(faPath, "amend", "-m", "new subject"); err != nil {
		t.Fatal(err)
	}
	subj := mustExec(t, e.c, faPath, "git", "log", "-1", "--format=%s")
	if subj != "new subject" {
		t.Errorf("subject = %q, expected %q", subj, "new subject")
	}
}

// TestAmendAllStagesTrackedModified: -a should pick up tracked-modified files
// without an explicit add (matches `git commit --amend -a`).
func TestAmendAllStagesTrackedModified(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "v1\n", "feat-a")

	e.writeFile(filepath.Join(faPath, "a.txt"), "v2\n")
	if _, err := e.gg(faPath, "amend", "-a", "--no-edit"); err != nil {
		t.Fatal(err)
	}
	contents := mustExec(t, e.c, faPath, "git", "show", "HEAD:a.txt")
	if contents != "v2" {
		t.Errorf("amend -a missed tracked-modified file; HEAD has %q", contents)
	}
}

// TestAmendRestacksDescendants pins the design decision: amend MUST restack
// because it rewrites the tip SHA, orphaning any descendant whose ParentSHA
// pointed at the old tip. After amending feat-a, feat-b's HEAD must differ
// from before AND feat-b's tree must contain both feat-a's amended state and
// its own commit.
func TestAmendRestacksDescendants(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "v1\n", "feat-a")
	e.ggMust(faPath, "append", "feat-b")
	fbPath := filepath.Join(e.work, "demo", "feat-b")
	e.commitInto(fbPath, "b.txt", "b\n", "feat-b")

	beforeFB := mustExec(t, e.c, fbPath, "git", "rev-parse", "HEAD")

	// Amend feat-a's content; should rewrite feat-a's tip and cascade.
	e.writeFile(filepath.Join(faPath, "a.txt"), "v2\n")
	if _, err := e.gg(faPath, "amend", "-a", "--no-edit"); err != nil {
		t.Fatal(err)
	}

	afterFB := mustExec(t, e.c, fbPath, "git", "rev-parse", "HEAD")
	if beforeFB == afterFB {
		t.Errorf("feat-b HEAD unchanged after amend on feat-a — restack didn't run (was %s)", beforeFB)
	}
	// feat-b should have feat-a's amended a.txt content.
	aOnFB := mustExec(t, e.c, fbPath, "git", "show", "HEAD:a.txt")
	if aOnFB != "v2" {
		t.Errorf("feat-b's a.txt = %q, expected the amended %q", aOnFB, "v2")
	}
	// feat-b's own b.txt should still be there.
	bOnFB := mustExec(t, e.c, fbPath, "git", "show", "HEAD:b.txt")
	if bOnFB != "b" {
		t.Errorf("feat-b lost its own b.txt after restack; got %q", bOnFB)
	}
}

// TestAmendOnLeafLeavesOtherStacksAlone: amending a leaf should not move any
// branch in an unrelated stack.
func TestAmendOnLeafLeavesOtherStacksAlone(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "a\n", "feat-a")
	// Independent stack rooted at trunk.
	e.ggMust(faPath, "new", "feat-x")
	fxPath := filepath.Join(e.work, "demo", "feat-x")
	e.commitInto(fxPath, "x.txt", "x\n", "feat-x")

	beforeFX := mustExec(t, e.c, fxPath, "git", "rev-parse", "HEAD")

	e.writeFile(filepath.Join(faPath, "a.txt"), "a-amended\n")
	if _, err := e.gg(faPath, "amend", "-a", "--no-edit"); err != nil {
		t.Fatal(err)
	}
	afterFX := mustExec(t, e.c, fxPath, "git", "rev-parse", "HEAD")
	if beforeFX != afterFX {
		t.Errorf("feat-x (unrelated stack) moved after amending feat-a: %s -> %s", beforeFX, afterFX)
	}
}

// TestAmendNoVerifyBypassesHook: a failing pre-commit hook blocks plain amend;
// --no-verify lets it through.
func TestAmendNoVerifyBypassesHook(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("hook script uses /bin/sh")
	}
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "v1\n", "feat-a")

	hooksDir := mustExec(t, e.c, faPath, "git", "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(faPath, hooksDir)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(hooksDir, "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.writeFile(filepath.Join(faPath, "a.txt"), "v2\n")
	mustExec(t, e.c, faPath, "git", "add", "a.txt")
	if _, err := e.gg(faPath, "amend", "--no-edit"); err == nil {
		t.Fatal("amend should have been rejected by pre-commit hook")
	}
	if _, err := e.gg(faPath, "amend", "--no-edit", "--no-verify"); err != nil {
		t.Fatalf("--no-verify should bypass hook: %v", err)
	}
	contents := mustExec(t, e.c, faPath, "git", "show", "HEAD:a.txt")
	if !strings.Contains(contents, "v2") {
		t.Errorf("amend with --no-verify didn't land; got %q", contents)
	}
}
