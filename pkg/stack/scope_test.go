package stack_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ashi-labs/gg/pkg/stack"
)

func TestSelect(t *testing.T) {
	l := sampleLineage()
	cases := []struct {
		name     string
		scope    stack.Scope
		from     string
		expected []string
	}{
		{"branch only", stack.ScopeBranch, "feat-a-2", []string{"feat-a-2"}},
		{
			"upstack from feat-a",
			stack.ScopeUpstack,
			"feat-a",
			[]string{"feat-a", "feat-a-1", "feat-a-2", "feat-a-2-x"},
		},
		{
			"downstack from leaf",
			stack.ScopeDownstack,
			"feat-a-2-x",
			[]string{"feat-a-2-x", "feat-a-2", "feat-a"},
		},
		{
			"full stack from leaf",
			stack.ScopeStack,
			"feat-a-2-x",
			[]string{"feat-a", "feat-a-1", "feat-a-2", "feat-a-2-x"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := l.Select(c.scope, c.from)
			sort.Strings(actual)
			sort.Strings(c.expected)
			if !reflect.DeepEqual(actual, c.expected) {
				t.Errorf("Select(%d, %q) = %v, expected %v", c.scope, c.from, actual, c.expected)
			}
		})
	}
}
