package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestFetchPicksUpRemoteCommit: a collaborator pushes to origin; `gg fetch`
// should update origin/<trunk> without touching the local trunk branch.
func TestFetchPicksUpRemoteCommit(t *testing.T) {
	t.Parallel()
	e := newEnv(t).withOwnUpstream()
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	scratch := e.work + "/collab"
	mustExec(t, e.c, "", "git", "clone", e.upstream, scratch)
	e.commitInto(scratch, "remote.txt", "hi", "remote commit")
	mustExec(t, e.c, scratch, "git", "push", "origin", "main")

	// Snapshot local trunk before; fetch shouldn't move it.
	beforeLocal := mustExec(t, e.c, primary, "git", "rev-parse", "main")

	if _, err := e.gg(primary, "fetch"); err != nil {
		t.Fatal(err)
	}

	// origin/main should now point at the remote commit.
	originHead := mustExec(t, e.c, primary, "git", "log", "-1", "--format=%s", "origin/main")
	if originHead != "remote commit" {
		t.Errorf("origin/main subject = %q, expected %q", originHead, "remote commit")
	}
	// Local main should NOT have moved (fetch ≠ pull).
	afterLocal := mustExec(t, e.c, primary, "git", "rev-parse", "main")
	if beforeLocal != afterLocal {
		t.Errorf("local main moved after fetch: %s -> %s", beforeLocal, afterLocal)
	}
}

// TestFetchPruneDropsDeletedRemoteRef: --prune should clean up
// remote-tracking refs whose upstream branch has been deleted.
func TestFetchPruneDropsDeletedRemoteRef(t *testing.T) {
	t.Parallel()
	e := newEnv(t).withOwnUpstream()
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	// Create + push a branch from a scratch clone, then pull it locally.
	scratch := e.work + "/collab"
	mustExec(t, e.c, "", "git", "clone", e.upstream, scratch)
	mustExec(t, e.c, scratch, "git", "checkout", "-b", "doomed")
	e.commitInto(scratch, "x.txt", "x", "doomed commit")
	mustExec(t, e.c, scratch, "git", "push", "-u", "origin", "doomed")

	// Pick up origin/doomed locally.
	if _, err := e.gg(primary, "fetch"); err != nil {
		t.Fatal(err)
	}
	refs := mustExec(t, e.c, primary, "git", "for-each-ref", "--format=%(refname)", "refs/remotes/")
	if !strings.Contains(refs, "refs/remotes/origin/doomed") {
		t.Fatalf("origin/doomed missing after fetch:\n%s", refs)
	}

	// Delete the branch on origin, then plain fetch — origin/doomed
	// should still be there (no prune yet).
	mustExec(t, e.c, scratch, "git", "push", "origin", "--delete", "doomed")
	if _, err := e.gg(primary, "fetch"); err != nil {
		t.Fatal(err)
	}
	refs = mustExec(t, e.c, primary, "git", "for-each-ref", "--format=%(refname)", "refs/remotes/")
	if !strings.Contains(refs, "refs/remotes/origin/doomed") {
		t.Errorf("plain fetch should not have pruned origin/doomed:\n%s", refs)
	}

	// Now with --prune, the stale ref should be dropped.
	if _, err := e.gg(primary, "fetch", "--prune"); err != nil {
		t.Fatal(err)
	}
	refs = mustExec(t, e.c, primary, "git", "for-each-ref", "--format=%(refname)", "refs/remotes/")
	if strings.Contains(refs, "refs/remotes/origin/doomed") {
		t.Errorf("--prune should have dropped origin/doomed:\n%s", refs)
	}
}

// TestFetchExplicitRemote: a positional remote arg restricts the fetch
// to that remote (matches `git fetch <remote>`).
func TestFetchExplicitRemote(t *testing.T) {
	t.Parallel()
	e := newEnv(t).withOwnUpstream()
	primary := e.ggMust(e.work, "clone", e.upstream, "demo")

	scratch := e.work + "/collab"
	mustExec(t, e.c, "", "git", "clone", e.upstream, scratch)
	e.commitInto(scratch, "remote.txt", "hi", "remote commit")
	mustExec(t, e.c, scratch, "git", "push", "origin", "main")

	if _, err := e.gg(primary, "fetch", "origin"); err != nil {
		t.Fatal(err)
	}
	originHead := mustExec(t, e.c, primary, "git", "log", "-1", "--format=%s", "origin/main")
	if originHead != "remote commit" {
		t.Errorf("origin/main subject = %q, expected %q", originHead, "remote commit")
	}
}

// TestFetchOutsideTrackedRepoErrors: gg fetch only makes sense inside a
// gg-managed repo, since it relies on the resolved repo context.
func TestFetchOutsideTrackedRepoErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	scratch := filepath.Join(e.work, "scratch")
	mustExec(t, e.c, "", "mkdir", "-p", scratch)

	if _, err := e.gg(scratch, "fetch"); err == nil {
		t.Fatal("expected fetch to error outside a tracked repo")
	}
}
