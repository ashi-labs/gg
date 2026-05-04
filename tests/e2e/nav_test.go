package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// buildStack creates: main ← feat-a ← feat-a-1 ← feat-a-2 and returns paths.
func buildStack(t *testing.T, e *env) (primary, fa, a1, a2 string) {
	t.Helper()
	primary = e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")
	fa = filepath.Join(e.work, "demo", "feat-a")
	e.ggMust(fa, "append", "feat-a-1")
	a1 = filepath.Join(e.work, "demo", "feat-a-1")
	e.ggMust(a1, "append", "feat-a-2")
	a2 = filepath.Join(e.work, "demo", "feat-a-2")
	return
}

func TestNavUpstreamDownstream(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary, fa, a1, a2 := buildStack(t, e)

	// upstream goes toward trunk (child → parent).
	if actual, _ := e.gg(a2, "upstream"); actual != a1 {
		t.Errorf("upstream from feat-a-2 = %q, expected %q", actual, a1)
	}
	// Alias "up" matches upstream.
	if actual, _ := e.gg(a2, "up"); actual != a1 {
		t.Errorf("up alias: actual %q, expected %q", actual, a1)
	}

	// downstream goes toward leaf (parent → child).
	if actual, _ := e.gg(fa, "downstream"); actual != a1 {
		t.Errorf("downstream from feat-a = %q, expected %q", actual, a1)
	}
	if actual, _ := e.gg(fa, "down"); actual != a1 {
		t.Errorf("down alias: actual %q, expected %q", actual, a1)
	}

	// Multi-step.
	if actual, _ := e.gg(a2, "upstream", "2"); actual != fa {
		t.Errorf("upstream 2 from feat-a-2 = %q, expected %q", actual, fa)
	}
	if actual, _ := e.gg(fa, "downstream", "2"); actual != a2 {
		t.Errorf("downstream 2 from feat-a = %q, expected %q", actual, a2)
	}
	// upstream caps at trunk.
	if actual, _ := e.gg(a2, "upstream", "99"); actual != primary {
		t.Errorf("upstream 99 from feat-a-2 = %q, expected %q (trunk)", actual, primary)
	}
}

func TestNavFirstLastTrunk(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary, fa, a1, a2 := buildStack(t, e)

	// first → root of the stack (direct child of trunk).
	if actual, _ := e.gg(a2, "first"); actual != fa {
		t.Errorf("first from feat-a-2 = %q, expected %q", actual, fa)
	}
	if actual, _ := e.gg(a1, "first"); actual != fa {
		t.Errorf("first from feat-a-1 = %q, expected %q", actual, fa)
	}

	// last → leaf.
	if actual, _ := e.gg(fa, "last"); actual != a2 {
		t.Errorf("last from feat-a = %q, expected %q", actual, a2)
	}
	if actual, _ := e.gg(a1, "last"); actual != a2 {
		t.Errorf("last from feat-a-1 = %q, expected %q", actual, a2)
	}

	// trunk → primary worktree.
	if actual, _ := e.gg(fa, "trunk"); actual != primary {
		t.Errorf("trunk from feat-a = %q, expected %q", actual, primary)
	}
	if actual, _ := e.gg(a2, "trunk"); actual != primary {
		t.Errorf("trunk from feat-a-2 = %q, expected %q", actual, primary)
	}
}

func TestNavRefusesFromTrunk(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")
	e.ggMust(primary, "append", "feat-a")

	// first / last from trunk need a stack selection first.
	for _, verb := range []string{"first", "last"} {
		_, err := e.gg(primary, verb)
		if err == nil {
			t.Errorf("%s from trunk should refuse", verb)
			continue
		}
		if !strings.Contains(err.Error(), "you're on trunk") {
			t.Errorf("%s from trunk: error should say 'you're on trunk', actual: %v", verb, err)
		}
	}
	// downstream from trunk with a single child descends into it (no picker).
	faPath := filepath.Join(e.work, "demo", "feat-a")
	if actual, _ := e.gg(primary, "downstream"); actual != faPath {
		t.Errorf("downstream from trunk (single child) = %q, expected %q", actual, faPath)
	}
	// Upstream / trunk from trunk mean "no-op, already here."
	for _, verb := range []string{"upstream", "trunk"} {
		_, err := e.gg(primary, verb)
		if err == nil || !strings.Contains(err.Error(), "already on trunk") {
			t.Errorf("%s from trunk: error should say 'already on trunk', actual: %v", verb, err)
		}
	}
}

func TestNavDownstreamFromEmptyTrunk(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	_, err := e.gg(primary, "downstream")
	if err == nil || !strings.Contains(err.Error(), "gg new") {
		t.Errorf("downstream from empty trunk: expected 'gg new' hint, actual: %v", err)
	}
}

func TestNavRefusesWhenAlreadyThere(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, fa, _, a2 := buildStack(t, e)

	// From the leaf: downstream / last should report "already the last entry".
	if _, err := e.gg(
		a2,
		"downstream",
	); err == nil ||
		!strings.Contains(err.Error(), "already the last entry in the stack") {
		t.Errorf(
			"downstream from leaf: expected 'already the last entry in the stack', actual: %v",
			err,
		)
	}
	if _, err := e.gg(
		a2,
		"last",
	); err == nil ||
		!strings.Contains(err.Error(), "already the last entry in the stack") {
		t.Errorf("last from leaf: expected 'already the last entry in the stack', actual: %v", err)
	}

	// From the first entry of the stack: first should report "already the first entry".
	if _, err := e.gg(
		fa,
		"first",
	); err == nil ||
		!strings.Contains(err.Error(), "already the first entry in the stack") {
		t.Errorf(
			"first from root-of-stack: expected 'already the first entry in the stack', actual: %v",
			err,
		)
	}
}
