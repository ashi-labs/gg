package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAddPath: a single explicit path stages just that path.
func TestAddPath(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "a.txt"), "a\n")
	e.writeFile(filepath.Join(primary, "b.txt"), "b\n")
	if _, err := e.gg(primary, "add", "a.txt"); err != nil {
		t.Fatal(err)
	}
	status := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	if !strings.Contains(status, "A  a.txt") {
		t.Errorf("a.txt should be staged:\n%s", status)
	}
	if !strings.Contains(status, "?? b.txt") {
		t.Errorf("b.txt should still be untracked:\n%s", status)
	}
}

// TestAddMultiplePaths: more than one path stages exactly those paths.
func TestAddMultiplePaths(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "a.txt"), "a\n")
	e.writeFile(filepath.Join(primary, "b.txt"), "b\n")
	e.writeFile(filepath.Join(primary, "c.txt"), "c\n")
	if _, err := e.gg(primary, "add", "a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	status := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	for _, want := range []string{"A  a.txt", "A  b.txt"} {
		if !strings.Contains(status, want) {
			t.Errorf("status missing %q:\n%s", want, status)
		}
	}
	if !strings.Contains(status, "?? c.txt") {
		t.Errorf("c.txt should still be untracked:\n%s", status)
	}
}

// TestAddAllStagesEverything: -a stages modified, deleted, and untracked
// across the whole worktree (matches `git add -A`).
func TestAddAllStagesEverything(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Three kinds of pending change to verify -a covers them all.
	//   - tracked-modified: README.md (seeded).
	//   - tracked-deleted: README.md... no, can't both modify and delete.
	//     Make a second tracked file first.
	e.writeFile(filepath.Join(primary, "doomed.txt"), "doomed\n")
	mustExec(t, e.c, primary, "git", "add", "doomed.txt")
	mustExec(t, e.c, primary, "git", "commit", "-m", "seed-doomed")

	e.writeFile(filepath.Join(primary, "README.md"), "modified\n")        // modified
	mustExec(t, e.c, primary, "rm", filepath.Join(primary, "doomed.txt")) // deleted
	e.writeFile(filepath.Join(primary, "fresh.txt"), "fresh\n")           // untracked

	if _, err := e.gg(primary, "add", "-a"); err != nil {
		t.Fatal(err)
	}
	status := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	for _, want := range []string{"M  README.md", "D  doomed.txt", "A  fresh.txt"} {
		if !strings.Contains(status, want) {
			t.Errorf("status missing %q:\n%s", want, status)
		}
	}
}

// TestAddAllRejectsPaths: -a is mutually exclusive with explicit paths;
// passing both should error before invoking git.
func TestAddAllRejectsPaths(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "a.txt"), "a\n")
	if _, err := e.gg(primary, "add", "-a", "a.txt"); err == nil {
		t.Fatal("expected `gg add -a a.txt` to error")
	}
	// And the worktree should be untouched (a.txt stays untracked, not staged).
	status := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	if !strings.Contains(status, "?? a.txt") {
		t.Errorf("a.txt should still be untracked after rejected add:\n%s", status)
	}
}

// TestAddNoArgsErrors: with no -a and no paths, gg add should refuse rather
// than fall through to `git add` (which would fail with its own confusing
// usage message).
func TestAddNoArgsErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	if _, err := e.gg(primary, "add"); err == nil {
		t.Fatal("expected `gg add` with no args to error")
	}
}

// completionLines drives cobra's `__complete` hidden command and returns
// just the suggestion lines (cobra's directive trailer ":<digit>" is
// dropped). Used by the completion tests below.
func completionLines(t *testing.T, e *env, dir string, args ...string) []string {
	t.Helper()
	out, err := e.gg(dir, append([]string{"__complete"}, args...)...)
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
	return lines
}

// TestAddCompletionFirstLineNotTruncated pins a regression: when the first
// porcelain line is " M path" (unstaged-modified, leading space), an
// over-eager TrimSpace upstream chopped the leading char off the path so
// completion offered "EADME.md" instead of "README.md".
func TestAddCompletionFirstLineNotTruncated(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// README.md is a tracked file from the seeded upstream. Modifying it
	// without staging puts a " M README.md" line at the top of porcelain
	// output — exactly the shape that triggered the bug.
	e.writeFile(filepath.Join(primary, "README.md"), "modified\n")

	lines := completionLines(t, e, primary, "add", "")
	for _, line := range lines {
		if line == "EADME.md" {
			t.Fatalf("completion truncated leading char: %q\nall: %v", line, lines)
		}
	}
	if !contains(lines, "README.md") {
		t.Errorf("completion missing README.md: %v", lines)
	}
}

// TestAddCompletionPathShapes covers the path-shape resolution: a plain
// prefix from the worktree root, a leading "./", and subdir-relative
// (where the user is `cd`'d into a subdir of the worktree).
func TestAddCompletionPathShapes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Build pkg/cli/root.go as an untracked file under a subdir.
	subdir := filepath.Join(primary, "pkg", "cli")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	e.writeFile(filepath.Join(subdir, "root.go"), "package cli\n")

	cases := []struct {
		name       string
		fromDir    string
		toComplete string
		want       string
	}{
		{"plain prefix from worktree root", primary, "pk", "pkg/cli/root.go"},
		{"./ prefix preserved", primary, "./pk", "./pkg/cli/root.go"},
		{"subdir-relative", subdir, "ro", "root.go"},
		{"./ in subdir", subdir, "./ro", "./root.go"},
		{"../ from subdir reaches sibling tree", subdir, "../cli/ro", "../cli/root.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := completionLines(t, e, tc.fromDir, "add", tc.toComplete)
			if !contains(lines, tc.want) {
				t.Errorf("completion for %q from %q: missing %q, got %v",
					tc.toComplete, tc.fromDir, tc.want, lines)
			}
		})
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
