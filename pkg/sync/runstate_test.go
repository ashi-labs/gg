package sync_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ashi-labs/gg/pkg/sync"
)

func TestRunStateRoundtrip(t *testing.T) {
	bare := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bare, "gg"), 0o755); err != nil {
		t.Fatal(err)
	}
	expected := &sync.RunState{
		Kind:               "sync",
		Trunk:              "main",
		InProgressBranch:   "feat-b",
		InProgressWorktree: "/wt/feat-b",
		Remaining:          []string{"feat-b", "feat-c"},
		Snapshots: map[string]string{
			"main":   "sha0",
			"feat-a": "sha1",
			"feat-b": "sha2",
			"feat-c": "sha3",
		},
	}
	if err := sync.Save(bare, expected); err != nil {
		t.Fatal(err)
	}
	actual, err := sync.Load(bare)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("roundtrip: actual %+v, expected %+v", actual, expected)
	}
}

func TestRunStateLoadMissing(t *testing.T) {
	rs, err := sync.Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load on missing file should not error, actual: %v", err)
	}
	if rs != nil {
		t.Errorf("Load on missing file should return nil, actual: %+v", rs)
	}
}

func TestRunStateClearIdempotent(t *testing.T) {
	bare := t.TempDir()
	// Clear when nothing is there.
	if err := sync.Clear(bare); err != nil {
		t.Errorf("Clear on missing file should be idempotent, actual: %v", err)
	}
	// Save then Clear.
	if err := sync.Save(bare, &sync.RunState{Kind: "sync", Trunk: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := sync.Clear(bare); err != nil {
		t.Fatal(err)
	}
	if rs, _ := sync.Load(bare); rs != nil {
		t.Error("Load after Clear should return nil")
	}
}
