package cli

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/tui/style"
)

// sample branches matching stack_test.go's shape:
//
//	main ← feat-a ← {feat-a-1, feat-a-2 ← feat-a-2-x}
//	main ← feat-b
func sampleBranches() []state.Branch {
	return []state.Branch{
		{Name: "feat-a", Parent: "main"},
		{Name: "feat-a-1", Parent: "feat-a"},
		{Name: "feat-a-2", Parent: "feat-a"},
		{Name: "feat-a-2-x", Parent: "feat-a-2"},
		{Name: "feat-b", Parent: "main"},
	}
}

func namesOf(branches []state.Branch) []string {
	out := make([]string, 0, len(branches))
	for _, b := range branches {
		out = append(out, b.Name)
	}
	sort.Strings(out)
	return out
}

func TestFilterToStack_FromLeaf(t *testing.T) {
	got, err := filterToStack("main", "feat-a-2-x", sampleBranches())
	if err != nil {
		t.Fatalf("filterToStack: %v", err)
	}
	want := []string{"feat-a", "feat-a-1", "feat-a-2", "feat-a-2-x"}
	if !reflect.DeepEqual(namesOf(got), want) {
		t.Errorf("names = %v, want %v", namesOf(got), want)
	}
}

func TestFilterToStack_FromRoot(t *testing.T) {
	got, err := filterToStack("main", "feat-a", sampleBranches())
	if err != nil {
		t.Fatalf("filterToStack: %v", err)
	}
	// Asking for the root of a stack should still pull in every descendant
	// (the whole stack), not just the root itself.
	want := []string{"feat-a", "feat-a-1", "feat-a-2", "feat-a-2-x"}
	if !reflect.DeepEqual(namesOf(got), want) {
		t.Errorf("names = %v, want %v", namesOf(got), want)
	}
}

func TestFilterToStack_SiblingStackExcluded(t *testing.T) {
	got, err := filterToStack("main", "feat-a-1", sampleBranches())
	if err != nil {
		t.Fatalf("filterToStack: %v", err)
	}
	for _, b := range got {
		if b.Name == "feat-b" {
			t.Error("feat-b belongs to a different stack and should be excluded")
		}
	}
}

func TestFilterToStack_TrunkRejected(t *testing.T) {
	if _, err := filterToStack("main", "main", sampleBranches()); err == nil {
		t.Error("expected error when focusing on trunk")
	}
}

func TestFilterToStack_UnknownRejected(t *testing.T) {
	if _, err := filterToStack("main", "ghost", sampleBranches()); err == nil {
		t.Error("expected error for unknown branch")
	}
}

func TestSortSiblings_AlphabeticalWhenNoRecency(t *testing.T) {
	names := []string{"charlie", "alpha", "bravo"}
	sortSiblings(names, nil)
	want := []string{"alpha", "bravo", "charlie"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestSortSiblings_RecencyDescending(t *testing.T) {
	names := []string{"alpha", "bravo", "charlie"}
	recency := map[string]int64{
		"alpha":   100,
		"bravo":   300,
		"charlie": 200,
	}
	sortSiblings(names, recency)
	want := []string{"bravo", "charlie", "alpha"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestSortSiblings_MissingRecencyEntriesSinkToBottom(t *testing.T) {
	names := []string{"alpha", "bravo", "charlie", "delta"}
	// Only bravo and delta have known activity. alpha and charlie should
	// sort to the bottom (still alphabetical between themselves).
	recency := map[string]int64{
		"bravo": 200,
		"delta": 100,
	}
	sortSiblings(names, recency)
	want := []string{"bravo", "delta", "alpha", "charlie"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestSortSiblings_TimestampTieBreaksByName(t *testing.T) {
	names := []string{"bravo", "alpha"}
	recency := map[string]int64{
		"alpha": 100,
		"bravo": 100,
	}
	sortSiblings(names, recency)
	want := []string{"alpha", "bravo"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestFormatCommitLine_ContainsAllParts(t *testing.T) {
	// Use a recent timestamp so compactAge produces a stable "Xm ago" string.
	c := gitx.CommitInfo{
		ShortSHA:      "abc1234",
		Subject:       "Wire up auth handler",
		UnixTimestamp: time.Now().Add(-30 * time.Minute).Unix(),
	}
	got := formatCommitLine(c, style.Stdout)
	for _, want := range []string{"abc1234", "30m ago", "Wire up auth handler"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatCommitLine missing %q in output: %q", want, got)
		}
	}
}

func TestFormatCommitLine_TruncatesLongSubject(t *testing.T) {
	c := gitx.CommitInfo{
		ShortSHA:      "abc1234",
		Subject:       strings.Repeat("x", 200),
		UnixTimestamp: time.Now().Unix(),
	}
	got := formatCommitLine(c, style.Stdout)
	// The subject is dim-styled; we just check the truncation marker landed
	// somewhere downstream of the SHA. truncate uses "…" when it trims.
	if !strings.Contains(got, "…") {
		t.Errorf("expected ellipsis in truncated output: %q", got)
	}
}
