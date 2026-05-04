package state_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/ashi-labs/gg/pkg/state"
)

// newBare initializes an empty bare repo in a test-scoped tempdir.
func newBare(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "test.git")
	if err := exec.Command("git", "init", "--bare", bare).Run(); err != nil {
		t.Fatalf("git init --bare: %v", err)
	}
	return bare
}

func TestRepoRoundtrip(t *testing.T) {
	bare := newBare(t)
	expected := state.Repo{
		Trunk:           "main",
		BareRepo:        bare,
		PrimaryWorktree: "/some/path",
		SyncStrategy:    "rebase",
		MergeStrategy:   "squash",
	}
	if err := state.SaveRepo(bare, expected); err != nil {
		t.Fatalf("SaveRepo: %v", err)
	}
	actual, err := state.LoadRepo(bare)
	if err != nil {
		t.Fatalf("LoadRepo: %v", err)
	}
	if actual != expected {
		t.Errorf("roundtrip: actual %+v, expected %+v", actual, expected)
	}
}

func TestBranchRoundtrip(t *testing.T) {
	bare := newBare(t)
	expected := state.Branch{
		Name:      "feat-a",
		Parent:    "main",
		ParentSHA: "abc123",
		Worktree:  "/p",
		PRNumber:  42,
	}
	if err := state.SaveBranch(bare, expected); err != nil {
		t.Fatalf("SaveBranch: %v", err)
	}
	actual, err := state.LoadBranch(bare, "feat-a")
	if err != nil {
		t.Fatalf("LoadBranch: %v", err)
	}
	if actual != expected {
		t.Errorf("roundtrip: actual %+v, expected %+v", actual, expected)
	}
}

func TestBranchWithDotsInName(t *testing.T) {
	bare := newBare(t)
	name := "feat.v2.prerelease"
	in := state.Branch{Name: name, Parent: "main", Worktree: "/x"}
	if err := state.SaveBranch(bare, in); err != nil {
		t.Fatalf("SaveBranch: %v", err)
	}
	actual, err := state.LoadBranch(bare, name)
	if err != nil {
		t.Fatalf("LoadBranch: %v", err)
	}
	if actual.Parent != "main" || actual.Worktree != "/x" {
		t.Errorf("dotted branch roundtrip lost data: %+v", actual)
	}
}

func TestAllBranches(t *testing.T) {
	bare := newBare(t)
	for _, n := range []string{"feat-a", "feat-b", "feat-c"} {
		if err := state.SaveBranch(bare, state.Branch{Name: n, Parent: "main"}); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := state.AllBranches(bare)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(actual))
	for _, b := range actual {
		names = append(names, b.Name)
	}
	sort.Strings(names)
	expected := []string{"feat-a", "feat-b", "feat-c"}
	if !reflect.DeepEqual(names, expected) {
		t.Errorf("names = %v, expected %v", names, expected)
	}
}

// AllBranches must skip records with no parent pointer (malformed state).
func TestAllBranchesSkipsParentless(t *testing.T) {
	bare := newBare(t)
	if err := state.SaveBranch(bare, state.Branch{Name: "ok", Parent: "main"}); err != nil {
		t.Fatal(err)
	}
	// Create a parentless entry via UpdateBranch.
	if err := state.UpdateBranch(bare, "orphan", func(b *state.Branch) {
		b.Worktree = "/somewhere"
	}); err != nil {
		t.Fatal(err)
	}
	bs, err := state.AllBranches(bare)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 || bs[0].Name != "ok" {
		t.Errorf("expected only 'ok', actual %+v", bs)
	}
}

func TestDeleteBranch(t *testing.T) {
	bare := newBare(t)
	_ = state.SaveBranch(bare, state.Branch{Name: "feat-a", Parent: "main"})
	if err := state.DeleteBranch(bare, "feat-a"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	b, _ := state.LoadBranch(bare, "feat-a")
	if b.Parent != "" {
		t.Errorf("still present: %+v", b)
	}
	// Idempotent.
	if err := state.DeleteBranch(bare, "feat-a"); err != nil {
		t.Errorf("second delete should no-op, actual %v", err)
	}
}

func TestRenameBranch(t *testing.T) {
	bare := newBare(t)
	_ = state.SaveBranch(
		bare,
		state.Branch{Name: "old", Parent: "main", Worktree: "/p", ParentSHA: "abc"},
	)
	if err := state.RenameBranch(bare, "old", "new"); err != nil {
		t.Fatal(err)
	}
	nb, _ := state.LoadBranch(bare, "new")
	if nb.Parent != "main" || nb.Worktree != "/p" || nb.ParentSHA != "abc" {
		t.Errorf("new branch lost fields: %+v", nb)
	}
	ob, _ := state.LoadBranch(bare, "old")
	if ob.Parent != "" {
		t.Errorf("old branch should be gone: %+v", ob)
	}
}

func TestUpdateParent(t *testing.T) {
	bare := newBare(t)
	_ = state.SaveBranch(
		bare,
		state.Branch{Name: "feat-a", Parent: "old", Worktree: "/p", ParentSHA: "sha1"},
	)
	if err := state.UpdateParent(bare, "feat-a", "new"); err != nil {
		t.Fatal(err)
	}
	b, _ := state.LoadBranch(bare, "feat-a")
	if b.Parent != "new" {
		t.Errorf("Parent = %q, expected new", b.Parent)
	}
	if b.Worktree != "/p" {
		t.Errorf("UpdateParent shouldn't touch Worktree, actual %q", b.Worktree)
	}
	if b.ParentSHA != "sha1" {
		t.Errorf("UpdateParent shouldn't touch ParentSHA, actual %q", b.ParentSHA)
	}
}

func TestUpdateParentSHAAndWorktree(t *testing.T) {
	bare := newBare(t)
	_ = state.SaveBranch(bare, state.Branch{Name: "feat-a", Parent: "main"})
	if err := state.UpdateParentSHA(bare, "feat-a", "newsha"); err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateWorktree(bare, "feat-a", "/new/path"); err != nil {
		t.Fatal(err)
	}
	b, _ := state.LoadBranch(bare, "feat-a")
	if b.ParentSHA != "newsha" || b.Worktree != "/new/path" {
		t.Errorf("partial updates: %+v", b)
	}
}

// Regression: writes used to shell out to git-config, which refuses to start
// when its cwd has been deleted (as happens after `gg fold` removes the
// worktree the user was standing in). The JSON backend doesn't rely on cwd,
// but keeping the test guards against a future regression.
func TestWriteFromDeletedCwd(t *testing.T) {
	bare := newBare(t)
	_ = state.SaveBranch(bare, state.Branch{Name: "feat-a", Parent: "main"})

	doomed := filepath.Join(t.TempDir(), "doomed")
	if err := os.Mkdir(doomed, 0o755); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	if err := os.Chdir(doomed); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(doomed); err != nil {
		t.Fatal(err)
	}

	if err := state.DeleteBranch(bare, "feat-a"); err != nil {
		t.Fatalf("DeleteBranch from deleted cwd: %v", err)
	}
	b, _ := state.LoadBranch(bare, "feat-a")
	if b.Parent != "" {
		t.Error("section should be removed even when cwd is deleted")
	}
}

// TestMigrateFromGitConfig seeds a bare repo's git config with legacy
// gg.* keys, calls into the package, and verifies the state is read
// back correctly and persisted to the JSON file.
func TestMigrateFromGitConfig(t *testing.T) {
	bare := newBare(t)
	configFile := filepath.Join(bare, "config")
	set := func(k, v string) {
		if err := exec.Command("git", "config", "--file", configFile, k, v).Run(); err != nil {
			t.Fatalf("git config %s %s: %v", k, v, err)
		}
	}
	set("gg.trunk", "main")
	set("gg.layout", "bare")
	set("gg.bare-repo", bare)
	set("gg.primary-worktree", "/primary")
	set("gg.branch.feat-a.parent", "main")
	set("gg.branch.feat-a.parent-sha", "abc123")
	set("gg.branch.feat-a.worktree", "/w/feat-a")
	set("gg.branch.feat-a.pr", "42")
	set("gg.branch.feat.dotted.parent", "main")

	repo, err := state.LoadRepo(bare)
	if err != nil {
		t.Fatalf("LoadRepo: %v", err)
	}
	if repo.Trunk != "main" || repo.PrimaryWorktree != "/primary" {
		t.Errorf("repo migrated incorrectly: %+v", repo)
	}
	b, err := state.LoadBranch(bare, "feat-a")
	if err != nil {
		t.Fatalf("LoadBranch feat-a: %v", err)
	}
	expected := state.Branch{
		Name: "feat-a", Parent: "main", ParentSHA: "abc123",
		Worktree: "/w/feat-a", PRNumber: 42,
	}
	if b != expected {
		t.Errorf("feat-a migrated incorrectly: actual %+v, expected %+v", b, expected)
	}
	db, _ := state.LoadBranch(bare, "feat.dotted")
	if db.Parent != "main" {
		t.Errorf("dotted branch did not migrate: %+v", db)
	}
	// Migration should have persisted the JSON file.
	if _, err := os.Stat(filepath.Join(bare, "gg.json")); err != nil {
		t.Errorf("migration should have written gg.json: %v", err)
	}
}

// TestConcurrentWrites fires many parallel mutators at a single bare repo
// and verifies every write landed. Exercises both the sync.Mutex-free design
// and the cross-fd flock serialization within a single process.
func TestConcurrentWrites(t *testing.T) {
	bare := newBare(t)
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			name := "feat-" + strconv.Itoa(i)
			if err := state.SaveBranch(
				bare,
				state.Branch{Name: name, Parent: "main"},
			); err != nil {
				t.Errorf("SaveBranch %s: %v", name, err)
			}
		}(i)
	}
	wg.Wait()
	bs, err := state.AllBranches(bare)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != n {
		t.Errorf("got %d branches, expected %d", len(bs), n)
	}
}
