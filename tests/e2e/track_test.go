package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTrackUntrack(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.ggMust(faPath, "append", "feat-a-1")
	a1Path := filepath.Join(e.work, "demo", "feat-a-1")

	// untrack the leaf — should succeed.
	if _, err := e.gg(a1Path, "untrack"); err != nil {
		t.Fatalf("untrack leaf: %v", err)
	}
	log, _ := e.gg(primary, "log")
	if strings.Contains(log, "feat-a-1") {
		t.Errorf("untracked branch still appears in log:\n%s", log)
	}

	// re-track with --parent.
	if _, err := e.gg(a1Path, "track", "--parent", "feat-a"); err != nil {
		t.Fatalf("track: %v", err)
	}
	log, _ = e.gg(primary, "log")
	if !strings.Contains(log, "feat-a-1") {
		t.Errorf("re-tracked branch missing from log:\n%s", log)
	}
}

func TestUntrackRefusesWithChildren(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	faPath := filepath.Join(e.work, "demo", "feat-a")
	e.ggMust(faPath, "append", "feat-a-1")

	_, err := e.gg(faPath, "untrack")
	if err == nil {
		t.Error("untrack of parent with children should refuse")
	} else if !strings.Contains(err.Error(), "tracked children") {
		t.Errorf("error should mention tracked children, actual: %v", err)
	}
}

func TestTrackRefusesTrunk(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	if _, err := e.gg(primary, "track"); err == nil {
		t.Error("track on trunk should refuse")
	}
}
