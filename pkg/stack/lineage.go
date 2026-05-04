package stack

import "github.com/ashi-labs/gg/pkg/state"

type Lineage struct {
	Trunk  string
	ByName map[string]state.Branch
}

func Build(trunk string, branches []state.Branch) Lineage {
	l := Lineage{
		Trunk:  trunk,
		ByName: make(map[string]state.Branch, len(branches)),
	}
	for _, b := range branches {
		l.ByName[b.Name] = b
	}
	return l
}

func (l Lineage) Names() []string {
	names := make([]string, 0, len(l.ByName))
	for n := range l.ByName {
		names = append(names, n)
	}
	return names
}

func (l Lineage) Parent(name string) string {
	if name == l.Trunk {
		return ""
	}
	if b, ok := l.ByName[name]; ok {
		return b.Parent
	}
	return ""
}

func (l Lineage) Children(name string) []string {
	var kids []string
	for n, b := range l.ByName {
		if b.Parent == name {
			kids = append(kids, n)
		}
	}
	return kids
}

func (l Lineage) Ancestors(name string) []string {
	var out []string
	cur := l.Parent(name)
	for cur != "" && cur != l.Trunk {
		out = append(out, cur)
		cur = l.Parent(cur)
	}
	if cur == l.Trunk {
		out = append(out, l.Trunk)
	}
	return out
}

func (l Lineage) Descendants(name string) []string {
	var out []string
	queue := l.Children(name)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		out = append(out, cur)
		queue = append(queue, l.Children(cur)...)
	}
	return out
}

func (l Lineage) Roots() []string {
	return l.Children(l.Trunk)
}

func (l Lineage) StackOf(name string) []string {
	if name == l.Trunk {
		return nil
	}
	root := name
	for {
		p := l.Parent(root)
		if p == "" || p == l.Trunk {
			break
		}
		root = p
	}
	out := []string{root}
	out = append(out, l.Descendants(root)...)
	return out
}

func (l Lineage) Topological() []string {
	var out []string
	var visit func(name string)
	visit = func(name string) {
		for _, c := range l.Children(name) {
			out = append(out, c)
			visit(c)
		}
	}
	visit(l.Trunk)
	return out
}

func (l Lineage) Contains(name string) bool {
	if name == l.Trunk {
		return true
	}
	_, ok := l.ByName[name]
	return ok
}
