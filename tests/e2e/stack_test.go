package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneAndCreate(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	// Nested layout: primary worktree lives at <container>/<trunk>, so a
	// plain `gg clone demo` emits a path ending in demo/main.
	if !strings.HasSuffix(primary, "demo/main") {
		t.Errorf("clone stdout = %q, expected path ending in 'demo/main'", primary)
	}
	if !e.exists(primary) {
		t.Errorf("primary worktree missing at %s", primary)
	}

	// Child of current (main).
	faPath := e.ggMust(primary, "append", "feat-a")
	if !e.exists(faPath) {
		t.Errorf("feat-a worktree missing at %s", faPath)
	}
	// Child of feat-a.
	a1Path := e.ggMust(faPath, "append", "feat-a-1")
	if !e.exists(a1Path) {
		t.Errorf("feat-a-1 worktree missing at %s", a1Path)
	}
	// new off trunk regardless of where you are.
	fbPath := e.ggMust(faPath, "new", "feat-b")
	if !e.exists(fbPath) {
		t.Errorf("feat-b worktree missing at %s", fbPath)
	}

	// Log should mention every branch.
	log, _ := e.gg(primary, "log")
	for _, name := range []string{"main", "feat-a", "feat-a-1", "feat-b"} {
		if !strings.Contains(log, name) {
			t.Errorf("log missing %q:\n%s", name, log)
		}
	}
}

func TestCreateVsNewSemantics(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")

	// append from feat-a → child of feat-a
	e.ggMust(faPath, "append", "feat-a-child")
	// new from feat-a → off trunk
	e.ggMust(faPath, "new", "feat-off-trunk")

	// Walk lineage via nav (mixing aliases and full names for coverage):
	//   feat-a-child's parent is feat-a (`create` stacks on current).
	//   feat-off-trunk's parent is trunk (`new` always roots on trunk).
	childUp, _ := e.gg(filepath.Join(e.work, "demo", "feat-a-child"), "up")
	if childUp != faPath {
		t.Errorf("feat-a-child up = %q, expected %q", childUp, faPath)
	}
	offUp, _ := e.gg(filepath.Join(e.work, "demo", "feat-off-trunk"), "upstream")
	if offUp != primary {
		t.Errorf("feat-off-trunk upstream = %q, expected %q (trunk)", offUp, primary)
	}
}
