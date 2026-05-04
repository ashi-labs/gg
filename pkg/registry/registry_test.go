package registry_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ashi-labs/gg/pkg/registry"
)

// withStateHome redirects the registry's on-disk path to a per-test tempdir.
// Tests can't share a global file.
func withStateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return dir
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	withStateHome(t)
	actual, err := registry.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(actual) != 0 {
		t.Errorf("expected empty, actual %d entries", len(actual))
	}
}

func TestUpsertInsertThenReplace(t *testing.T) {
	withStateHome(t)
	e := registry.Entry{
		Name:            "demo",
		Bare:            "/tmp/demo.git",
		PrimaryWorktree: "/tmp/demo",
		Trunk:           "main",
		Origin:          "git@host:u/d.git",
	}
	if err := registry.Upsert(e); err != nil {
		t.Fatal(err)
	}
	actual, _ := registry.Load()
	if len(actual) != 1 || actual[0].Name != "demo" {
		t.Fatalf("expected one entry named demo, actual %+v", actual)
	}
	if actual[0].AddedAt.IsZero() || actual[0].LastUsedAt.IsZero() {
		t.Error("Upsert should set AddedAt/LastUsedAt when zero")
	}

	// Update trunk; AddedAt should be preserved.
	origAdded := actual[0].AddedAt
	time.Sleep(time.Millisecond)
	e.Trunk = "master"
	if err := registry.Upsert(e); err != nil {
		t.Fatal(err)
	}
	actual, _ = registry.Load()
	if len(actual) != 1 {
		t.Errorf("expected still one entry, actual %d", len(actual))
	}
	if actual[0].Trunk != "master" {
		t.Errorf("trunk not updated: %+v", actual[0])
	}
	if !actual[0].AddedAt.Equal(origAdded) {
		t.Errorf(
			"AddedAt should be preserved across replaces: was %v, now %v",
			origAdded,
			actual[0].AddedAt,
		)
	}
}

func TestTouchUpdatesLastUsed(t *testing.T) {
	withStateHome(t)
	e := registry.Entry{
		Name:            "demo",
		Bare:            "/tmp/demo.git",
		PrimaryWorktree: "/tmp/demo",
		Trunk:           "main",
	}
	_ = registry.Upsert(e)
	actual, _ := registry.Load()
	before := actual[0].LastUsedAt

	time.Sleep(2 * time.Millisecond)
	if err := registry.Touch("/tmp/demo.git"); err != nil {
		t.Fatal(err)
	}
	actual, _ = registry.Load()
	if !actual[0].LastUsedAt.After(before) {
		t.Errorf("LastUsedAt should advance: before=%v after=%v", before, actual[0].LastUsedAt)
	}
}

func TestTouchMissingIsNoOp(t *testing.T) {
	withStateHome(t)
	if err := registry.Touch("/nonexistent.git"); err != nil {
		t.Errorf("Touch on missing entry should be a no-op, actual: %v", err)
	}
}

func TestRemove(t *testing.T) {
	withStateHome(t)
	_ = registry.Upsert(
		registry.Entry{Name: "a", Bare: "/tmp/a.git", PrimaryWorktree: "/tmp/a", Trunk: "main"},
	)
	_ = registry.Upsert(
		registry.Entry{Name: "b", Bare: "/tmp/b.git", PrimaryWorktree: "/tmp/b", Trunk: "main"},
	)
	if err := registry.Remove("/tmp/a.git"); err != nil {
		t.Fatal(err)
	}
	actual, _ := registry.Load()
	if len(actual) != 1 || actual[0].Name != "b" {
		t.Errorf("expected only b to remain, actual %+v", actual)
	}
	// Idempotent: removing the same entry again is a no-op.
	if err := registry.Remove("/tmp/a.git"); err != nil {
		t.Errorf("second Remove should be no-op, actual: %v", err)
	}
}

func TestValidate(t *testing.T) {
	dir := t.TempDir()
	bare := filepath.Join(dir, "demo.git")
	primary := filepath.Join(dir, "demo")
	_ = os.Mkdir(bare, 0o755)
	_ = os.Mkdir(primary, 0o755)

	e := registry.Entry{Bare: bare, PrimaryWorktree: primary}
	if actual := e.Validate(); actual != registry.StatusOK {
		t.Errorf("all paths exist, expected StatusOK, actual %v", actual)
	}
	_ = os.RemoveAll(primary)
	if actual := e.Validate(); actual != registry.StatusPrimaryMissing {
		t.Errorf("primary gone, expected StatusPrimaryMissing, actual %v", actual)
	}
	_ = os.RemoveAll(bare)
	if actual := e.Validate(); actual != registry.StatusBareMissing {
		t.Errorf("bare gone, expected StatusBareMissing, actual %v", actual)
	}
}

func TestSaveSortsByLastUsedDesc(t *testing.T) {
	withStateHome(t)
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	_ = registry.Upsert(
		registry.Entry{
			Name:            "old",
			Bare:            "/tmp/o.git",
			PrimaryWorktree: "/tmp/o",
			Trunk:           "main",
			LastUsedAt:      older,
		},
	)
	_ = registry.Upsert(
		registry.Entry{
			Name:            "new",
			Bare:            "/tmp/n.git",
			PrimaryWorktree: "/tmp/n",
			Trunk:           "main",
			LastUsedAt:      newer,
		},
	)
	actual, _ := registry.Load()
	if actual[0].Name != "new" {
		t.Errorf("expected newest first, actual: %+v", actual)
	}
}
