package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRestoreDiscardsWorkingTreeEdits: with no flag, restore reverts the
// working tree's copy of a path back to its index/HEAD content.
func TestRestoreDiscardsWorkingTreeEdits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	original, err := e.readFile(filepath.Join(primary, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	e.writeFile(filepath.Join(primary, "README.md"), "edited\n")

	if _, err := e.gg(primary, "restore", "README.md"); err != nil {
		t.Fatal(err)
	}
	got, err := e.readFile(filepath.Join(primary, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got != original {
		t.Errorf("README.md not restored; got %q want %q", got, original)
	}
}

// TestRestoreStagedUnstagesPath: --staged moves a staged change back to
// unstaged (working tree keeps the edit; index drops it).
func TestRestoreStagedUnstagesPath(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "new.txt"), "hi\n")
	mustExec(t, e.c, primary, "git", "add", "new.txt")
	statusBefore := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	if !strings.Contains(statusBefore, "A  new.txt") {
		t.Fatalf("setup: expected new.txt staged-as-added:\n%s", statusBefore)
	}

	if _, err := e.gg(primary, "restore", "--staged", "new.txt"); err != nil {
		t.Fatal(err)
	}
	statusAfter := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	if !strings.Contains(statusAfter, "?? new.txt") {
		t.Errorf("--staged should have moved new.txt back to untracked:\n%s", statusAfter)
	}
}

// TestRestoreFromSource: --source=<branch> rewrites a path's content to
// what it was on that branch. Useful for "give me this file the way it
// was on main".
func TestRestoreFromSource(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")

	// Diverge: feat-a writes its own version of README.md.
	e.commitInto(faPath, "README.md", "from-feat-a\n", "feat-a edit")

	// Now restore README.md on feat-a from main; should yield main's version.
	if _, err := e.gg(faPath, "restore", "--source", "main", "README.md"); err != nil {
		t.Fatal(err)
	}
	got, err := e.readFile(filepath.Join(faPath, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "from-feat-a") {
		t.Errorf("--source=main should have replaced feat-a's content; got %q", got)
	}
}

// TestRestoreNoArgsErrors: restore requires at least one path; no-arg
// invocation should refuse rather than fall through to git's confusing
// usage error.
func TestRestoreNoArgsErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	if _, err := e.gg(primary, "restore"); err == nil {
		t.Fatal("expected `gg restore` with no args to error")
	}
}

// TestRestoreCompletionDefaultOffersDirty: the default mode (no flags)
// only does meaningful work on dirty paths; completion should reflect
// that. Clean tracked files should NOT appear.
func TestRestoreCompletionDefaultOffersDirty(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Add a clean tracked file (committed) so we have a known
	// "shouldn't appear" candidate. README.md is the seed file.
	e.writeFile(filepath.Join(primary, "clean.txt"), "x\n")
	mustExec(t, e.c, primary, "git", "add", "clean.txt")
	mustExec(t, e.c, primary, "git", "commit", "-m", "add clean")
	// And a dirty tracked file (modify the seed README).
	e.writeFile(filepath.Join(primary, "README.md"), "modified\n")

	out, err := e.gg(primary, "__complete", "restore", "")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		lines = append(lines, line)
	}
	foundDirty := false
	for _, name := range lines {
		if name == "README.md" {
			foundDirty = true
		}
		if name == "clean.txt" {
			t.Errorf("default restore completion offered clean.txt (no-op target): %v", lines)
		}
	}
	if !foundDirty {
		t.Errorf("default restore completion missing README.md: %v", lines)
	}
}

// TestRestoreCompletionWithSourceOffersTracked: --source=<ref> rewrites
// a path's content from another ref, so even clean files are valid
// targets. Completion should expand to the full tracked set.
func TestRestoreCompletionWithSourceOffersTracked(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Add a clean tracked file. With --source, this should appear in
	// completion even though it's not dirty.
	e.writeFile(filepath.Join(primary, "clean.txt"), "x\n")
	mustExec(t, e.c, primary, "git", "add", "clean.txt")
	mustExec(t, e.c, primary, "git", "commit", "-m", "add clean")

	out, err := e.gg(primary, "__complete", "restore", "--source=main", "")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		lines = append(lines, line)
	}
	foundClean := false
	for _, name := range lines {
		if name == "clean.txt" {
			foundClean = true
		}
	}
	if !foundClean {
		t.Errorf("--source restore completion missing clean tracked file clean.txt: %v", lines)
	}
}
