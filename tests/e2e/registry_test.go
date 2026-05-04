package e2e_test

import (
	"strings"
	"testing"
)

func TestCloneRegistersRepo(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	data, err := e.readFile(e.state + "/gg/repos.toml")
	if err != nil {
		t.Fatalf("registry file missing after clone: %v", err)
	}
	for _, expected := range []string{`name = "demo"`, primary, `trunk = "main"`} {
		if !strings.Contains(data, expected) {
			t.Errorf("registry missing %q:\n%s", expected, data)
		}
	}
}

func TestReposListShowsEveryClone(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.ggMust(e.work, "clone", e.upstream, "alpha")
	e.ggMust(e.work, "clone", e.upstream, "beta")

	out, err := e.gg(e.work, "repos", "--list")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"alpha", "beta", "main"} {
		if !strings.Contains(out, expected) {
			t.Errorf("repos --list missing %q:\n%s", expected, out)
		}
	}
}

func TestCdByName(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// From a different working dir: `gg cd demo` should print demo's primary.
	actual, err := e.gg("/tmp", "cd", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if actual != primary {
		t.Errorf("gg cd demo = %q, expected %q", actual, primary)
	}
}

func TestCdUnknownRepoFails(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.ggMust(e.work, "clone", e.upstream, "demo")

	if _, err := e.gg(e.work, "cd", "ghost"); err == nil {
		t.Error("cd to unknown repo should fail")
	}
}

func TestCleanupRemovesMissing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.ggMust(e.work, "clone", e.upstream, "demo")

	// Wipe the whole container from disk; the registry entry is now stale.
	mustExec(t, e.c, "", "rm", "-rf", e.work+"/demo")

	if _, err := e.gg(e.work, "cleanup", "--yes"); err != nil {
		t.Fatal(err)
	}
	out, _ := e.gg(e.work, "repos", "--list")
	if strings.Contains(out, "demo") {
		t.Errorf("demo still in registry after cleanup:\n%s", out)
	}
}

func TestLinkRewritesMovedRepo(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")

	// Move the whole container to a new parent dir. Nested layout makes this
	// trivial: the container is self-contained (bare + worktrees all live
	// underneath), so a single mv relocates everything.
	newParent := e.work + "/moved"
	mustExec(t, e.c, "", "mkdir", "-p", newParent)
	mustExec(t, e.c, "", "mv", e.work+"/demo", newParent+"/demo")

	// Link from the new location's primary worktree.
	newPrimary := newParent + "/demo/main"
	if _, err := e.gg(newPrimary, "link"); err != nil {
		t.Fatal(err)
	}

	// feat-a's recorded worktree path should now be under the new parent.
	faConfigPath := newParent + "/demo/.bare/gg.json"
	cfg, err := e.readFile(faConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	wantWT := newParent + "/demo/feat-a"
	if !strings.Contains(cfg, wantWT) {
		t.Errorf("config not rewritten to contain %q:\n%s", wantWT, cfg)
	}
	// Registry should now point at the new bare.
	reg, _ := e.readFile(e.state + "/gg/repos.toml")
	if !strings.Contains(reg, newParent+"/demo/.bare") {
		t.Errorf("registry not updated:\n%s", reg)
	}
	// And cd should route to the new location. With one stack (feat-a), cd
	// auto-picks the stack's first entry.
	wantFA := newParent + "/demo/feat-a"
	if actual, _ := e.gg("/tmp", "cd", "demo"); actual != wantFA {
		t.Errorf("cd after link = %q, expected %q", actual, wantFA)
	}
}

func TestCdWithNoStacksGoesToPrimary(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// No stacks yet — cd should land at primary.
	actual, err := e.gg("/tmp", "cd", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if actual != primary {
		t.Errorf("cd with no stacks = %q, expected %q (primary)", actual, primary)
	}
}
