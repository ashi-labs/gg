package stack_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
)

// sampleLineage: main ← feat-a ← {feat-a-1, feat-a-2 ← feat-a-2-x}, main ← feat-b.
func sampleLineage() stack.Lineage {
	return stack.Build("main", []state.Branch{
		{Name: "feat-a", Parent: "main"},
		{Name: "feat-a-1", Parent: "feat-a"},
		{Name: "feat-a-2", Parent: "feat-a"},
		{Name: "feat-a-2-x", Parent: "feat-a-2"},
		{Name: "feat-b", Parent: "main"},
	})
}

func TestParent(t *testing.T) {
	l := sampleLineage()
	cases := map[string]string{
		"feat-a":      "main",
		"feat-a-1":    "feat-a",
		"feat-a-2-x":  "feat-a-2",
		"main":        "",
		"nonexistent": "",
	}
	for branch, expected := range cases {
		if actual := l.Parent(branch); actual != expected {
			t.Errorf("Parent(%q) = %q, expected %q", branch, actual, expected)
		}
	}
}

func TestChildren(t *testing.T) {
	l := sampleLineage()
	cases := map[string][]string{
		"main":       {"feat-a", "feat-b"},
		"feat-a":     {"feat-a-1", "feat-a-2"},
		"feat-a-2":   {"feat-a-2-x"},
		"feat-a-1":   nil,
		"feat-a-2-x": nil,
	}
	for branch, expected := range cases {
		actual := l.Children(branch)
		sort.Strings(actual)
		sort.Strings(expected)
		if len(actual) == 0 && len(expected) == 0 {
			continue
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Errorf("Children(%q) = %v, expected %v", branch, actual, expected)
		}
	}
}

func TestAncestors(t *testing.T) {
	actual := sampleLineage().Ancestors("feat-a-2-x")
	expected := []string{"feat-a-2", "feat-a", "main"}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Ancestors = %v, expected %v", actual, expected)
	}
}

func TestDescendants(t *testing.T) {
	actual := sampleLineage().Descendants("feat-a")
	sort.Strings(actual)
	expected := []string{"feat-a-1", "feat-a-2", "feat-a-2-x"}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Descendants = %v, expected %v", actual, expected)
	}
}

func TestRoots(t *testing.T) {
	actual := sampleLineage().Roots()
	sort.Strings(actual)
	expected := []string{"feat-a", "feat-b"}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Roots = %v, expected %v", actual, expected)
	}
}

func TestStackOf(t *testing.T) {
	actual := sampleLineage().StackOf("feat-a-2-x")
	sort.Strings(actual)
	expected := []string{"feat-a", "feat-a-1", "feat-a-2", "feat-a-2-x"}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("StackOf = %v, expected %v", actual, expected)
	}
}

func TestTopological(t *testing.T) {
	l := sampleLineage()
	order := l.Topological()
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	for n, b := range l.ByName {
		if b.Parent == "" || b.Parent == l.Trunk {
			continue
		}
		if pos[b.Parent] >= pos[n] {
			t.Errorf(
				"%s (pos %d) should come before %s (pos %d)",
				b.Parent,
				pos[b.Parent],
				n,
				pos[n],
			)
		}
	}
	// All tracked branches present.
	if len(order) != len(l.ByName) {
		t.Errorf("topological len = %d, expected %d", len(order), len(l.ByName))
	}
}

func TestContains(t *testing.T) {
	l := sampleLineage()
	if !l.Contains("main") {
		t.Error("Contains(main) should be true (trunk)")
	}
	if !l.Contains("feat-a") {
		t.Error("Contains(feat-a) should be true")
	}
	if l.Contains("ghost") {
		t.Error("Contains(ghost) should be false")
	}
}
