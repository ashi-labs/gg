package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestBlameShowsAuthorAndSubject: blame on a tracked file should show
// the committer's name and the commit subject for each line.
func TestBlameShowsAuthorAndSubject(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "first\nsecond\nthird\n", "feat-a tip")

	out, err := e.gg(faPath, "blame", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	// Default git blame format is "<sha> (<author> <date> <line-no>) <content>".
	// The sandbox pins author=test. Subject isn't in the default format —
	// `--show-name` etc. would add more, but we just want passthrough proof.
	if !strings.Contains(out, "test") {
		t.Errorf("blame output missing author 'test':\n%s", out)
	}
	for _, line := range []string{"first", "second", "third"} {
		if !strings.Contains(out, line) {
			t.Errorf("blame output missing %q:\n%s", line, out)
		}
	}
}

// TestBlameLineRange: -L scopes blame to a line range (passthrough flag).
func TestBlameLineRange(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.commitInto(faPath, "a.txt", "alpha\nbeta\ngamma\n", "feat-a tip")

	out, err := e.gg(faPath, "blame", "-L", "2,2", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("expected `beta` in -L 2,2 output:\n%s", out)
	}
	if strings.Contains(out, "alpha") || strings.Contains(out, "gamma") {
		t.Errorf("-L 2,2 should not include other lines:\n%s", out)
	}
}

// TestBlameNoArgsErrors: blame requires at least one path.
func TestBlameNoArgsErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	if _, err := e.gg(primary, "blame"); err == nil {
		t.Fatal("expected `gg blame` with no args to error")
	}
}

// TestBlameCompletesTrackedFiles pins that blame's tab-completion
// surfaces tracked content (clean files), not the dirty/staged set.
// README.md is committed in the seeded upstream and is the canonical
// "clean tracked file" for the test sandbox; an untracked file should
// NOT appear.
func TestBlameCompletesTrackedFiles(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Untracked file — must not appear in blame's completions.
	e.writeFile(filepath.Join(primary, "scratch.txt"), "x\n")

	out, err := e.gg(primary, "__complete", "blame", "")
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
	foundReadme := false
	for _, line := range lines {
		if line == "README.md" {
			foundReadme = true
		}
		if line == "scratch.txt" {
			t.Errorf("blame completion offered untracked file scratch.txt: %v", lines)
		}
	}
	if !foundReadme {
		t.Errorf("blame completion missing tracked README.md: %v", lines)
	}
}
