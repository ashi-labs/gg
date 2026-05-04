package stack

type Scope int

const (
	ScopeStack Scope = iota
	ScopeUpstack
	ScopeDownstack
	ScopeBranch
)

func (l Lineage) Select(scope Scope, name string) []string {
	switch scope {
	case ScopeBranch:
		return []string{name}
	case ScopeUpstack:
		return append([]string{name}, l.Descendants(name)...)
	case ScopeDownstack:
		out := []string{name}
		for cur := l.Parent(name); cur != "" && cur != l.Trunk; cur = l.Parent(cur) {
			out = append(out, cur)
		}
		return out
	default:
		return l.StackOf(name)
	}
}
