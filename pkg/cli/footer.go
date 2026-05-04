package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ashi-labs/gg/pkg/stack"
)

const (
	footerStart = "<!-- gg:stack-start -->"
	footerEnd   = "<!-- gg:stack-end -->"
)

func render(current, trunk string, lineage stack.Lineage, prs map[string]int) string {
	var buf strings.Builder
	buf.WriteString(footerStart)
	buf.WriteString("\n## gg stack\n\n")
	root := current
	for {
		p := lineage.Parent(root)
		if p == "" || p == trunk {
			break
		}
		root = p
	}
	renderStack(&buf, root, current, 0, lineage, prs)
	buf.WriteString(footerEnd)
	return strings.TrimSpace(buf.String())
}

func renderStack(
	buf *strings.Builder,
	name, current string, depth int,
	lineage stack.Lineage,
	prs map[string]int,
) {
	pr := prs[name]
	indent := strings.Repeat("  ", depth)
	var line string
	switch {
	case name == current && pr > 0:
		marker := " 👈 this PR"
		line = fmt.Sprintf("%s- **#%d `%s`%s**", indent, pr, name, marker)
	case pr > 0:
		line = fmt.Sprintf("%s- #%d `%s`", indent, pr, name)
	default:
		// no pr yet
		line = fmt.Sprintf("%s- `%s` _(not submitted)_", indent, name)
	}
	buf.WriteString(line)
	buf.WriteByte('\n')
	children := lineage.Children(name)
	sort.Strings(children)
	for _, child := range children {
		renderStack(buf, child, current, depth+1, lineage, prs)
	}
}

func footerBounds(body string) (int, int) {
	return strings.Index(body, footerStart), strings.Index(body, footerEnd)
}

func bodyBeforeAndAfter(body string, start, end int) (string, string) {
	return strings.TrimSpace(body[:start]), strings.TrimSpace(body[end:])
}

func stripFooter(body string) string {
	start, end := footerBounds(body)
	if start < 0 || end <= start {
		return body
	}
	end += len(footerEnd)
	before, after := bodyBeforeAndAfter(body, start, end)
	return strings.TrimSpace(fmt.Sprintf("%s\n%s", before, after))
}

func withUpdatedFooter(
	body, current, trunk string,
	lineage stack.Lineage,
	prs map[string]int,
) string {
	stripped := stripFooter(body)
	footer := render(current, trunk, lineage, prs)
	return fmt.Sprintf("%s\n\n%s\n", stripped, footer)
}
