package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestStashPushPopRoundtrip: stash a tracked-modified file, verify the
// worktree returns to clean, then pop and verify the modification reappears.
func TestStashPushPopRoundtrip(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "README.md"), "modified\n")

	if _, err := e.gg(primary, "stash"); err != nil {
		t.Fatal(err)
	}
	status := mustExec(t, e.c, primary, "git", "status", "--porcelain")
	if status != "" {
		t.Errorf("worktree should be clean after stash, status:\n%s", status)
	}

	if _, err := e.gg(primary, "stash", "pop"); err != nil {
		t.Fatal(err)
	}
	contents, err := e.readFile(filepath.Join(primary, "README.md"))
	if err != nil || !strings.Contains(contents, "modified") {
		t.Errorf("expected modification back in README.md, got %q (err=%v)", contents, err)
	}
}

// TestStashIncludeUntracked: by default `gg stash` leaves untracked files
// behind; with -u it sweeps them in too.
func TestStashIncludeUntracked(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "README.md"), "modified\n")
	e.writeFile(filepath.Join(primary, "fresh.txt"), "fresh\n")

	// Plain stash — fresh.txt should still be on disk.
	if _, err := e.gg(primary, "stash"); err != nil {
		t.Fatal(err)
	}
	if !e.exists(filepath.Join(primary, "fresh.txt")) {
		t.Errorf("plain stash should leave untracked files alone")
	}
	// Bring back the tracked-modified change so the next stash has work.
	if _, err := e.gg(primary, "stash", "pop"); err != nil {
		t.Fatal(err)
	}

	// With -u: untracked goes too.
	if _, err := e.gg(primary, "stash", "-u"); err != nil {
		t.Fatal(err)
	}
	if e.exists(filepath.Join(primary, "fresh.txt")) {
		t.Errorf("-u should sweep up untracked files")
	}
}

// TestStashListAndDrop: list shows entries; drop removes the top one without
// applying it.
func TestStashListAndDrop(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "README.md"), "v1\n")
	if _, err := e.gg(primary, "stash", "-m", "first"); err != nil {
		t.Fatal(err)
	}
	e.writeFile(filepath.Join(primary, "README.md"), "v2\n")
	if _, err := e.gg(primary, "stash", "-m", "second"); err != nil {
		t.Fatal(err)
	}

	out, err := e.gg(primary, "stash", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"first", "second"} {
		if !strings.Contains(out, want) {
			t.Errorf("stash list missing %q:\n%s", want, out)
		}
	}

	// Drop the top entry; "second" should disappear, "first" should remain.
	if _, err := e.gg(primary, "stash", "drop"); err != nil {
		t.Fatal(err)
	}
	out, err = e.gg(primary, "stash", "list")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "second") {
		t.Errorf("drop should have removed `second`:\n%s", out)
	}
	if !strings.Contains(out, "first") {
		t.Errorf("drop should have left `first`:\n%s", out)
	}
}

// TestStashListGroupsByBranchCurrentFirst: stashes are repo-global, but
// `gg stash list` groups them by the branch they were pushed from with
// the current worktree's branch first. Other branches print in
// alphabetical order.
func TestStashListGroupsByBranchCurrentFirst(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-zzz")
	zzzPath := filepath.Join(e.work, "demo", "feat-zzz")
	e.ggMust(primary, "append", "feat-aaa")
	aaaPath := filepath.Join(e.work, "demo", "feat-aaa")

	// Push one stash from each location: main, feat-zzz, feat-aaa.
	e.writeFile(filepath.Join(primary, "README.md"), "trunk-edit\n")
	if _, err := e.gg(primary, "stash", "-m", "trunk-stash"); err != nil {
		t.Fatal(err)
	}
	e.writeFile(filepath.Join(zzzPath, "z.txt"), "z\n")
	if _, err := e.gg(zzzPath, "stash", "-u", "-m", "zzz-stash"); err != nil {
		t.Fatal(err)
	}
	e.writeFile(filepath.Join(aaaPath, "a.txt"), "a\n")
	if _, err := e.gg(aaaPath, "stash", "-u", "-m", "aaa-stash"); err != nil {
		t.Fatal(err)
	}

	// From feat-aaa, expected order: feat-aaa (current) first, then
	// feat-zzz and main alphabetically (feat-zzz < main).
	out := e.ggMust(aaaPath, "stash", "list")

	// Sanity: all three branches' stashes show up (repo-global).
	for _, want := range []string{"aaa-stash", "zzz-stash", "trunk-stash"} {
		if !strings.Contains(out, want) {
			t.Errorf("stash list missing %q:\n%s", want, out)
		}
	}

	// Order: feat-aaa header before feat-zzz before main.
	idxAaa := strings.Index(out, "feat-aaa")
	idxZzz := strings.Index(out, "feat-zzz")
	idxMain := strings.Index(out, "main")
	if idxAaa < 0 || idxZzz < 0 || idxMain < 0 {
		t.Fatalf("missing branch header in:\n%s", out)
	}
	if !(idxAaa < idxZzz && idxZzz < idxMain) {
		t.Errorf("group order wrong (want feat-aaa < feat-zzz < main):\n%s", out)
	}
}

// TestStashShowDiff: `gg stash show -p` should print the diff of the top
// stash, including the modified path.
func TestStashShowDiff(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	e.writeFile(filepath.Join(primary, "README.md"), "stashed-content\n")
	if _, err := e.gg(primary, "stash"); err != nil {
		t.Fatal(err)
	}
	out, err := e.gg(primary, "stash", "show", "-p")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "README.md") || !strings.Contains(out, "+stashed-content") {
		t.Errorf("stash show -p missing diff content:\n%s", out)
	}
}
