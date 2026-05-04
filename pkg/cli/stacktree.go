package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/charmbracelet/lipgloss/tree"
)

// stackRow is one fully-rendered line of the stack tree plus the structured
// data needed to act on it. Callers (log, checkout picker, future tree
// consumers) zip these to their own per-row concerns — status columns in
// `gg log`, selection metadata in the picker, etc.
//
// When the renderer is asked to expand commits (gg ls -a), each branch's
// header line is followed by zero or more rows with IsCommit=true. Those
// rows carry the parent branch's name in Branch but no Path/Current data —
// the columns are skipped during table render.
type stackRow struct {
	Branch   string // branch name; for the trunk row this is repo.Trunk
	Path     string // worktree path (primary worktree for trunk)
	Current  bool   // this branch is the one currently checked out
	Label    string // normal rendering: tree scaffolding + styled name
	IsCommit bool   // sub-row showing one commit under the branch above
	// HoverLabel is Label re-rendered with the branch name (not the marker,
	// not the tree scaffolding) underlined. The picker uses this for the
	// cursor row so the row keeps its own foreground color while still being
	// visually picked out.
	HoverLabel string
}

type stackNode struct {
	name        string
	path        string
	commitLines int
}

// renderStackRows builds the stack tree with lipgloss/tree and returns one
// row per rendered output line, in top-to-bottom order. The palette argument
// selects which stream's styles to use (stdout for `gg log`, stderr for the
// picker — see pkg/tui/style for why the two streams are separate).
//
// sortByRecency, when non-nil, replaces the default alphabetical sibling
// order with a most-recent-commit-first sort (ties broken by name). Used by
// `gg ls -a` so the eye lands on hot work first.
//
// commitLinesByBranch, when non-nil, makes the renderer pack pre-formatted
// commit lines into each branch's tree-node value (multiline). lipgloss/tree
// extends the gutter through subsequent lines, so commit lines stay aligned
// under their branch with no manual scaffold computation. The returned rows
// stay 1:1 with rendered output lines so column-cell zipping in renderLogTable
// remains straightforward — IsCommit marks the rows where columns should be
// skipped.
//
// The trunk is always the first row. Two parallel trees are rendered — one
// with plain name cells, one with the branch-name text underlined — because
// lipgloss doesn't compose styles onto already-rendered ANSI strings. Cost
// is negligible for stack-sized trees.
func renderStackRows(
	lineage stack.Lineage,
	current string,
	pal style.Palette,
	sortByRecency map[string]int64,
	commitLinesByBranch map[string][]string,
) []stackRow {
	var nodes []stackNode

	base := tree.Root(trunkLabel(repo.Trunk, current, pal, false))
	hover := tree.Root(trunkLabel(repo.Trunk, current, pal, true))
	nodes = append(nodes, stackNode{name: repo.Trunk, path: repo.PrimaryWorktree})

	roots := lineage.Roots()
	sortSiblings(roots, sortByRecency)
	for _, r := range roots {
		bc, hc := buildBranchNodePair(
			lineage,
			r,
			current,
			pal,
			sortByRecency,
			commitLinesByBranch,
			&nodes,
		)
		base.Child(bc)
		hover.Child(hc)
	}
	// See log.go for the note on overriding the default enumerator style —
	// we intentionally drop the library's PaddingRight(1) so `├──` sits
	// flush against the name cell.
	//
	// The hover tree uses Dirty instead of Dim so a hovered row reads
	// uniformly — marker + name + leading scaffolding all in the caret
	// accent. Only the name gets the extra Underline (see branchLabel).
	base.EnumeratorStyle(pal.Dim)
	hover.EnumeratorStyle(pal.Dirty)

	baseLines := strings.Split(strings.TrimRight(base.String(), "\n"), "\n")
	hoverLines := strings.Split(strings.TrimRight(hover.String(), "\n"), "\n")

	var rows []stackRow
	li := 0
	for _, n := range nodes {
		rows = append(rows, stackRow{
			Branch:     n.name,
			Path:       n.path,
			Current:    n.name == current,
			Label:      safeLine(baseLines, li),
			HoverLabel: safeLine(hoverLines, li),
		})
		li++
		for k := 0; k < n.commitLines; k++ {
			rows = append(rows, stackRow{
				Branch:     n.name,
				IsCommit:   true,
				Label:      safeLine(baseLines, li),
				HoverLabel: safeLine(hoverLines, li),
			})
			li++
		}
	}
	return rows
}

// safeLine returns lines[i] or "" if i is out of range. Defensive against
// any future skew between the nodeEntry count and tree.String() output (e.g.
// a hidden node being introduced); we'd rather emit a blank label than
// panic and kill the whole render.
func safeLine(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i]
}

// buildBranchNodePair recurses the lineage starting at `name`, returning a
// matched pair of tree nodes (plain + hover-underlined) and appending this
// node (then each descendant, in pre-order) to nodes. The two trees stay
// structurally identical so their rendered lines align 1:1 in the caller.
//
// commitLinesByBranch[name], when non-empty, is folded into the node's value
// as a multiline string. lipgloss/tree applies the proper gutter to every
// non-first line, so commit lines under a branch indent under that branch's
// scaffolding without any per-row prefix math here.
func buildBranchNodePair(
	lineage stack.Lineage,
	name, current string,
	pal style.Palette,
	sortByRecency map[string]int64,
	commitLinesByBranch map[string][]string,
	nodes *[]stackNode,
) (*tree.Tree, *tree.Tree) {
	baseLabel := branchLabel(name, current, pal, false)
	hoverLabel := branchLabel(name, current, pal, true)
	kids := lineage.Children(name)
	sortSiblings(kids, sortByRecency)
	commitLines := commitLinesByBranch[name]
	if len(commitLines) > 0 {
		decorated := make([]string, len(commitLines))
		for i, l := range commitLines {
			sym := "├"
			if len(kids) == 0 && i == len(commitLines)-1 {
				sym = "└"
			}
			decorated[i] = fmt.Sprintf("%s %s", pal.Dim.Render(sym), l)
		}
		joined := strings.Join(decorated, "\n")
		baseLabel = baseLabel + "\n" + joined
		hoverLabel = hoverLabel + "\n" + joined
	}
	base := tree.Root(baseLabel)
	hover := tree.Root(hoverLabel)
	b, _ := state.LoadBranch(bare, name)
	*nodes = append(*nodes, stackNode{name: name, path: b.Worktree, commitLines: len(commitLines)})
	for _, c := range kids {
		bc, hc := buildBranchNodePair(
			lineage,
			c,
			current,
			pal,
			sortByRecency,
			commitLinesByBranch,
			nodes,
		)
		base.Child(bc)
		hover.Child(hc)
	}
	return base, hover
}

// sortSiblings orders names alphabetically when recency is nil, or by most
// recent commit timestamp first (with name as a stable tiebreaker) when
// recency is provided. Names absent from the recency map sort to the bottom
// — they're effectively "no known activity" and shouldn't crowd the top.
func sortSiblings(names []string, recency map[string]int64) {
	if recency == nil {
		sort.Strings(names)
		return
	}
	sort.SliceStable(names, func(i, j int) bool {
		ti, oki := recency[names[i]]
		tj, okj := recency[names[j]]
		if oki != okj {
			return oki
		}
		if ti != tj {
			return ti > tj
		}
		return names[i] < names[j]
	})
}

// trunkLabel renders the trunk's name cell.
//
// Color rules:
//   - hover=true: Dirty (matches the picker's caret) with the name
//     underlined — so a hovered row reads uniformly with the caret on
//     its right.
//   - otherwise: Trunk (yellow); or Current (purple) when the trunk is
//     also the checked-out branch.
//
// The hover branch never applies in `gg log` (it passes hover=false for
// every row) but is the picker's cursor-row rendering.
func trunkLabel(trunk, current string, pal style.Palette, hover bool) string {
	// Marker never changes for the trunk row — it stays the Trunk glyph
	// whether the user is physically on the branch or not. Only the
	// color flips to Current when trunk == current to communicate
	// "you're here." Previously we also swapped the marker to
	// Glyphs.Current, which invisibly collapsed to `●` in the unicode
	// set but hid the nerd-font branch icon — inconsistent with how
	// `gg cd` renders the same row (cd always passes current="" here).
	marker := style.Glyphs.Trunk + " "
	if hover {
		return pal.Dirty.Render(marker) + pal.Dirty.Underline(true).Render(trunk)
	}
	st := pal.Trunk
	if trunk == current {
		st = pal.Current
	}
	return st.Render(marker + trunk)
}

// branchLabel renders a non-trunk branch's name cell.
//
// Color rules:
//   - hover=true: Dirty with the name underlined (picker cursor row).
//   - current branch: Current (purple) with a filled dot.
//   - everything else: Branch (blue) with a hollow dot.
func branchLabel(name, current string, pal style.Palette, hover bool) string {
	marker := style.Glyphs.Branch + " "
	if hover {
		return pal.Dirty.Render(marker) + pal.Dirty.Underline(true).Render(name)
	}
	st := pal.Branch
	if name == current {
		marker = style.Glyphs.CurrentBranch + " "
		st = pal.Current
	}
	return st.Render(marker + name)
}
