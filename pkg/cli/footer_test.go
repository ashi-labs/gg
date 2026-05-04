package cli

import (
	"strings"
	"testing"

	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
)

// sample: main ← feat-a ← feat-a-1, main ← feat-a ← feat-a-2.
func testSampleLineage() stack.Lineage {
	return stack.Build("main", []state.Branch{
		{Name: "feat-a", Parent: "main"},
		{Name: "feat-a-1", Parent: "feat-a"},
		{Name: "feat-a-2", Parent: "feat-a"},
	})
}

func TestRenderFooterMarksSelf(t *testing.T) {
	prs := map[string]int{"feat-a": 10, "feat-a-1": 11, "feat-a-2": 12}
	out := withUpdatedFooter("", "feat-a-1", "main", testSampleLineage(), prs)
	if !strings.Contains(out, "**#11 `feat-a-1` 👈 this PR**") {
		t.Errorf("expected feat-a-1 line to be bolded with marker:\n%s", out)
	}
	// Non-self PRs stay plain.
	if !strings.Contains(out, "- #10 `feat-a`\n") {
		t.Errorf("feat-a should render plain:\n%s", out)
	}
	if !strings.Contains(out, "- #12 `feat-a-2`") {
		t.Errorf("feat-a-2 should appear as sibling:\n%s", out)
	}
}

func TestRenderFooterIncludesRootAndDescendants(t *testing.T) {
	prs := map[string]int{"feat-a": 10, "feat-a-1": 11}
	// self = feat-a-1 — the tree should include its root (feat-a) and siblings.
	out := withUpdatedFooter("", "feat-a-1", "main", testSampleLineage(), prs)
	for _, expected := range []string{"feat-a", "feat-a-1", "feat-a-2"} {
		if !strings.Contains(out, expected) {
			t.Errorf("footer missing %q:\n%s", expected, out)
		}
	}
	// Indentation: feat-a at top level, children indented.
	if !strings.Contains(out, "\n  - ") {
		t.Errorf("expected at least one indented child line:\n%s", out)
	}
}

func TestRenderFooterUnsubmittedBranch(t *testing.T) {
	// Only feat-a has a PR; children don't yet.
	prs := map[string]int{"feat-a": 10}
	out := withUpdatedFooter("", "feat-a-1", "main", testSampleLineage(), prs)
	if !strings.Contains(out, "`feat-a-1` _(not submitted)_") {
		t.Errorf("unsubmitted children should show _(not submitted)_:\n%s", out)
	}
}

func TestAppendsToEmpty(t *testing.T) {
	out := withUpdatedFooter("", "feat-a", "main", testSampleLineage(), map[string]int{"feat-a": 1})
	if !strings.Contains(out, footerStart) ||
		!strings.Contains(out, footerEnd) {
		t.Errorf("markers missing: %s", out)
	}
}

func TestAppendsAfterUserBody(t *testing.T) {
	body := "## Summary\n\nFixes the thing."
	out := withUpdatedFooter(
		body,
		"feat-a",
		"main",
		testSampleLineage(),
		map[string]int{"feat-a": 1},
	)
	if !strings.HasPrefix(out, "## Summary") {
		t.Errorf("user body should be preserved at the top:\n%s", out)
	}
	if !strings.Contains(out, footerStart) {
		t.Errorf("footer should be appended:\n%s", out)
	}
}

func TestIdempotent(t *testing.T) {
	body := "Body."
	out := withUpdatedFooter(
		body,
		"feat-a",
		"main",
		testSampleLineage(),
		map[string]int{"feat-a": 1},
	)
	expected := strings.Clone(out)
	actual := withUpdatedFooter(
		out,
		"feat-a",
		"main",
		testSampleLineage(),
		map[string]int{"feat-a": 1},
	)
	if expected != actual {
		t.Errorf("footer rendering not idempotent:\nexpected:\n%s\nactual:\n%s", expected, actual)
	}
}
