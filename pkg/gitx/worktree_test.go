package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitInit runs git in dir with deterministic identity, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.t",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestRemove_ReadOnlySeededCache reproduces the prune bug: a worktree that
// contains a read-only seeded directory (mode 0555, like Go's module cache)
// makes `git worktree remove --force` abort, leaving the directory on disk.
// Remove must fall back to a forced delete so the dir is gone, the admin
// entry is pruned, and the branch ref becomes deletable.
func TestRemove_ReadOnlySeededCache(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "bare.git")

	// Bootstrap a bare repo with one commit so worktrees can attach.
	seed := filepath.Join(root, "seed")
	gitRun(t, root, "init", "-q", seed)
	gitRun(t, seed, "commit", "-q", "--allow-empty", "-m", "init")
	gitRun(t, root, "clone", "-q", "--bare", seed, bare)

	wt := filepath.Join(root, "feature")
	gitRun(t, bare, "worktree", "add", "-q", wt, "-b", "feature")

	// Seed a read-only cache dir, the way a CoW-cloned module cache lands.
	cache := filepath.Join(wt, "cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "lib"), []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cache, 0o555); err != nil {
		t.Fatal(err)
	}
	// Restore perms on cleanup so t.TempDir removal doesn't fail.
	t.Cleanup(func() { _ = os.Chmod(cache, 0o755) })

	// Sanity: plain `git worktree remove --force` should choke on this.
	if err := Worktree.Remove(bare, wt); err != nil {
		t.Fatalf("Remove should recover from a read-only cache, got: %v", err)
	}

	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir should be gone, stat err = %v", err)
	}
	// Admin entry pruned → branch ref is now deletable.
	if list := gitRun(t, bare, "worktree", "list"); strings.Contains(list, "feature") {
		t.Fatalf("worktree admin entry should be pruned, list:\n%s", list)
	}
	gitRun(t, bare, "branch", "-D", "feature") // would fail if still checked out
}

func TestForceRemoveDir_MissingPathIsNoError(t *testing.T) {
	if err := forceRemoveDir(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatalf("missing path should be a no-op, got: %v", err)
	}
	if err := forceRemoveDir(""); err != nil {
		t.Fatalf("empty path should be a no-op, got: %v", err)
	}
}
