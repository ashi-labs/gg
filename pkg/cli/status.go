package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ashi-labs/gg/pkg/config"
	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/gitx/forge"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/sync"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"stat"},
		Short:   "Show stack position, working tree, and what needs attention.",
		Long: `status (stat) prints a compact "you are here" view tailored to stacked
work — the worktree path, lineage in the stack, ahead/behind the
parent, working-tree summary, and any actionable hints (paused sync,
parent moved since last restack, PR open with unpushed commits, etc.).

PR statuses come from gg's local cache (same one ` + "`gg log`" + ` populates),
so this stays fast. Run ` + "`gg log`" + ` or wait for the precmd prefetch to
freshen the cache.`,
		Args: cobra.NoArgs,
		RunE: runStatus,
	}
}

func runStatus(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	cfg := config.Load()
	current, err := gitx.Revision.CurrentBranch(cwd)
	if err != nil {
		return err
	}
	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}
	lineage := stack.Build(repo.Trunk, branches)

	var blocks []string
	blocks = append(blocks, renderHeaderBlock())
	if rs, _ := sync.Load(bare); rs != nil {
		blocks = append(blocks, renderPausedBlock(rs))
	}
	if l := renderLineageBlock(lineage, current, cfg.Status); l != "" {
		blocks = append(blocks, l)
	}
	if ab := renderAheadBehindBlock(lineage, current, cfg.Status); ab != "" {
		blocks = append(blocks, ab)
	}
	blocks = append(blocks, renderWorkingTreeBlock())
	if hints := renderHintsBlock(lineage, current, branches, cfg); hints != "" {
		blocks = append(blocks, hints)
	}
	nonEmptyBlocks := filterEmpty(blocks)
	if len(nonEmptyBlocks) != 0 {
		stdout("\n" + strings.Join(nonEmptyBlocks, "\n\n") + "\n")
	}
	return nil
}

func filterEmpty(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// renderHeaderBlock prints the worktree section: a `worktree` badge
// header followed by the file glyph and ~/-shortened cwd. The branch
// itself is shown inside the lineage block, so this header is purely
// "where on disk am I."
func renderHeaderBlock() string {
	fileGlyph := style.Stdout.Branch.Render(style.Glyphs.File)
	path := style.Stdout.Branch.Render(tildePath(cwd))
	return style.Stdout.Badge.Render("worktree") + "\n  " + fileGlyph + " " + path
}

// tildePath replaces the user's home prefix with `~` so paths stay
// readable without losing absolute meaning. Falls back to the input
// unchanged when home isn't resolvable or doesn't match.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}

// renderPausedBlock surfaces an in-progress sync/restack at the top of
// the output, with the next-step commands. Only called when runstate
// exists.
func renderPausedBlock(rs *sync.RunState) string {
	headline := style.Stdout.Dirty.Render("⚠ " + rs.Kind + " paused on " + rs.InProgressBranch)
	hint := "  resolve conflicts then `gg continue`, or `gg abort` to bail"
	return headline + "\n" + hint
}

// renderLineageBlock prints `lineage\n  <chain>` as a single arrow-
// separated line. On trunk: just the trunk node with a `(trunk)`
// annotation. On a tracked non-trunk branch: trunk → … → parent →
// current → child → … → leaf, picking one child path through the
// lineage with a `(+N siblings)` marker when forks exist. Truncates
// on either end at status.max-stack-depth (skipped when 0).
func renderLineageBlock(l stack.Lineage, current string, sc config.StatusConfig) string {
	if current == "" || current == "HEAD" {
		return ""
	}
	header := style.Stdout.Badge.Render("lineage")
	if current == repo.Trunk {
		node := style.Stdout.Trunk.Render(style.Glyphs.Trunk) +
			" " + style.Stdout.Trunk.Render(current) +
			" " + style.Stdout.Dim.Render("(trunk)")
		return header + "\n  " + node
	}
	if !l.Contains(current) {
		// Untracked branch: just render the branch in isolation —
		// stack relationships aren't known.
		node := style.Stdout.Current.Render(style.Glyphs.CurrentBranch) +
			" " + style.Stdout.Current.Render(current)
		return header + "\n  " + node
	}

	// Build trunk → … → parent in topological order.
	ancestors := l.Ancestors(current)
	for i, j := 0, len(ancestors)-1; i < j; i, j = i+1, j-1 {
		ancestors[i], ancestors[j] = ancestors[j], ancestors[i]
	}
	cap := sc.MaxStackDepth
	ancestors = capHead(ancestors, cap)
	descendants := descendChain(l, current)
	descendants = capTail(descendants, cap)

	chain := append([]string{}, ancestors...)
	chain = append(chain, current)
	chain = append(chain, descendants...)

	var parts []string
	for _, n := range chain {
		// capHead/capTail markers and sibling-annotated entries are
		// already rendered with style; pass them through verbatim.
		if strings.HasPrefix(n, "\x1b") || strings.Contains(n, " (+") {
			parts = append(parts, n)
			continue
		}
		parts = append(parts, renderLineageNode(n, current))
	}
	arrow := " " + style.Stdout.Dim.Render("→") + " "
	return header + "\n  " + strings.Join(parts, arrow)
}

// renderLineageNode formats a branch as `<glyph> <name>` with the
// palette appropriate to its role in the lineage.
func renderLineageNode(name, current string) string {
	switch name {
	case current:
		return style.Stdout.Current.Render(style.Glyphs.CurrentBranch) +
			" " + style.Stdout.Current.Render(name)
	case repo.Trunk:
		return style.Stdout.Trunk.Render(style.Glyphs.Trunk) +
			" " + style.Stdout.Trunk.Render(name)
	default:
		return style.Stdout.Branch.Render(style.Glyphs.Branch) +
			" " + style.Stdout.Branch.Render(name)
	}
}

// descendChain walks one child at a time so the linear lineage view
// stays readable. When a branch has multiple children, picks the first
// alphabetically so the output is deterministic, and appends a "(+N
// sibling[s])" annotation so the user knows the rest exist.
func descendChain(l stack.Lineage, name string) []string {
	var out []string
	cur := name
	for {
		kids := l.Children(cur)
		if len(kids) == 0 {
			return out
		}
		sort.Strings(kids)
		next := kids[0]
		label := renderLineageNode(next, "")
		if extras := len(kids) - 1; extras > 0 {
			noun := "siblings"
			if extras == 1 {
				noun = "sibling"
			}
			label += style.Stdout.Dim.Render(fmt.Sprintf(" (+%d %s)", extras, noun))
		}
		out = append(out, label)
		cur = next
	}
}

func capHead(s []string, cap int) []string {
	if cap <= 0 || len(s) <= cap {
		return s
	}
	out := []string{style.Stdout.Dim.Render(fmt.Sprintf("…+%d", len(s)-cap))}
	return append(out, s[len(s)-cap:]...)
}

func capTail(s []string, cap int) []string {
	if cap <= 0 || len(s) <= cap {
		return s
	}
	out := append([]string{}, s[:cap]...)
	return append(out, style.Stdout.Dim.Render(fmt.Sprintf("…+%d", len(s)-cap)))
}

// renderAheadBehindBlock emits the `ahead-behind` block, with one
// indented line per comparison target. Comparison set is driven by
// status.ahead-behind-against (parent, trunk, or both). Skipped on
// trunk and on untracked branches — neither has a meaningful parent
// distance.
func renderAheadBehindBlock(l stack.Lineage, current string, sc config.StatusConfig) string {
	if current == "" || current == "HEAD" || current == repo.Trunk || !l.Contains(current) {
		return ""
	}
	parent := l.Parent(current)
	if parent == "" {
		return ""
	}
	wantParent := sc.AheadBehindAgainst == config.StatusAheadBehindParent ||
		sc.AheadBehindAgainst == config.StatusAheadBehindBoth
	wantTrunk := sc.AheadBehindAgainst == config.StatusAheadBehindTrunk ||
		sc.AheadBehindAgainst == config.StatusAheadBehindBoth
	parts := []string{style.Stdout.Badge.Render("ahead-behind")}
	if wantTrunk && parent != repo.Trunk {
		if line := aheadBehindRow(repo.Trunk, current, true); line != "" {
			parts = append(parts, line)
		}
	}
	if wantParent {
		if line := aheadBehindRow(parent, current, false); line != "" {
			parts = append(parts, line)
		}
	}
	if len(parts) == 1 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// aheadBehindRow renders one row: `  <glyph> <branch>  <count phrase>`.
// trunkStyle picks the trunk palette so the row reads the same way the
// lineage block does.
func aheadBehindRow(left, right string, trunkStyle bool) string {
	count := aheadBehindPhrase(left, right)
	if count == "" {
		return ""
	}
	var glyph, name string
	if trunkStyle {
		glyph = style.Stdout.Trunk.Render(style.Glyphs.Trunk)
		name = style.Stdout.Trunk.Render(left)
	} else {
		glyph = style.Stdout.Branch.Render(style.Glyphs.Branch)
		name = style.Stdout.Branch.Render(left)
	}
	return fmt.Sprintf("  %s %s is %s", glyph, name, count)
}

// aheadBehindPhrase renders just the count phrase ("in sync", "3
// behind", "2 ahead", "3 behind · 1 ahead"). Returns "" on git error.
func aheadBehindPhrase(left, right string) string {
	ahead, behind, err := gitx.Ref.AheadBehind(cwd, left, right)
	if err != nil {
		return ""
	}
	switch {
	case ahead == 0 && behind == 0:
		return style.Stdout.Success.Render("in sync")
	case ahead == 0:
		return fmt.Sprintf("%d behind", behind)
	case behind == 0:
		return style.Stdout.Dirty.Render(fmt.Sprintf("%d ahead", ahead))
	default:
		return style.Stdout.Dirty.Render(fmt.Sprintf("%d ahead", ahead)) +
			" · " + fmt.Sprintf("%d behind", behind)
	}
}

// renderWorkingTreeBlock parses `git status --porcelain` into the
// staged/unstaged/untracked buckets and renders them under the `diff`
// badge. Each entry shows the porcelain status code colored by what it
// means: A=Success (added), D=Danger (deleted), M=Trunk-yellow
// (modified). Other codes (R, C, U, T) render in neutral foreground.
func renderWorkingTreeBlock() string {
	raw, err := gitx.In(cwd).Cmd("status", "--porcelain", "--untracked-files=all").Bytes()
	if err != nil {
		return ""
	}
	var staged, unstaged, untracked []string
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		x, y, path := line[0], line[1], line[3:]
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		switch {
		case x == '?' && y == '?':
			untracked = append(untracked, style.Stdout.Dim.Render("? "+path))
		default:
			if x != ' ' && x != '?' {
				staged = append(staged, formatStatusEntry(x, path))
			}
			if y != ' ' && y != '?' {
				unstaged = append(unstaged, formatStatusEntry(y, path))
			}
		}
	}
	header := style.Stdout.Badge.Render("diff")
	if len(staged) == 0 && len(unstaged) == 0 && len(untracked) == 0 {
		return header + "\n  " + style.Stdout.Success.Render("clean")
	}
	var b strings.Builder
	b.WriteString(header)
	sep := "\n    "
	for _, bucket := range []struct {
		label string
		items []string
	}{
		{"staged", staged},
		{"unstaged", unstaged},
		{"untracked", untracked},
	} {
		if len(bucket.items) == 0 {
			continue
		}
		b.WriteString("\n  " + bucket.label)
		b.WriteString(sep + strings.Join(bucket.items, sep))
	}
	return b.String()
}

// formatStatusEntry renders a single porcelain entry as `<code> <path>`,
// painting both halves with the palette that matches the operation.
// Unknown codes pass through with neutral styling.
func formatStatusEntry(code byte, path string) string {
	codeStr := string(code)
	switch code {
	case 'A':
		return style.Stdout.Success.Render(codeStr) + " " + style.Stdout.Success.Render(path)
	case 'D':
		return style.Stdout.Danger.Render(codeStr) + " " + style.Stdout.Danger.Render(path)
	case 'M':
		return style.Stdout.Trunk.Render(codeStr) + " " + style.Stdout.Trunk.Render(path)
	default:
		return codeStr + " " + path
	}
}

// renderHintsBlock surfaces only the things that need user action.
// Empty string when nothing is wrong — the surrounding blocks already
// say "you're fine" by their absence of warnings.
func renderHintsBlock(
	l stack.Lineage,
	current string,
	branches []state.Branch,
	cfg config.Config,
) string {
	if current == "" || current == "HEAD" {
		return ""
	}
	var hints []string

	if !l.Contains(current) {
		hints = append(hints, fmt.Sprintf("%s isn't tracked by gg — run `gg track` to add it",
			styleBranch(current)))
		return style.Stdout.Badge.Render("needs attention") + "\n  " + strings.Join(hints, "\n  ")
	}

	if current != repo.Trunk {
		if cur, ok := l.ByName[current]; ok {
			parentTip, err := gitx.Revision.HeadSHA(bare, cur.Parent)
			if err == nil && cur.ParentSHA != "" && parentTip != cur.ParentSHA {
				hints = append(hints, fmt.Sprintf(
					"%s moved since last restack — run `gg restack`",
					styleBranch(cur.Parent),
				))
			}
		}
	}

	if ahead, behind, err := gitx.Ref.AheadBehind(cwd, "origin/"+current, current); err == nil {
		switch {
		case ahead > 0 && behind == 0:
			hints = append(hints, fmt.Sprintf(
				"%d commit(s) ahead of origin/%s — run `gg submit` to push",
				ahead, current,
			))
		case behind > 0 && ahead == 0:
			hints = append(hints, fmt.Sprintf(
				"%d commit(s) behind origin/%s — `gg sync` to fold them in",
				behind, current,
			))
		case ahead > 0 && behind > 0:
			hints = append(hints, fmt.Sprintf(
				"diverged from origin/%s (%d ahead, %d behind)",
				current, ahead, behind,
			))
		}
	}

	if pr := prHintFor(current, branches, cfg); pr != "" {
		hints = append(hints, pr)
	}

	if len(hints) == 0 {
		return ""
	}
	return style.Stdout.Badge.Render("needs attention") + "\n  " + strings.Join(hints, "\n  ")
}

// prHintFor surfaces the PR for the current branch when it's actionable
// (cache says CLOSED or MERGED). For OPEN/DRAFT or a cold cache we
// return "" — those aren't problems, they're just steady states, and
// asserting "PR #N open" without verifying would risk being wrong.
func prHintFor(current string, branches []state.Branch, cfg config.Config) string {
	var prNum int
	for _, b := range branches {
		if b.Name == current {
			prNum = b.PRNumber
			break
		}
	}
	if prNum == 0 {
		return ""
	}
	cached := loadFreshPRStatuses(cfg, false)
	st, ok := cached[current]
	if !ok {
		return ""
	}
	return prHintLine(prNum, st)
}

func prHintLine(num int, st forge.PRStatus) string {
	switch st.State {
	case "MERGED":
		return fmt.Sprintf("PR #%d MERGED — `gg sync` will prune this branch", num)
	case "CLOSED":
		return fmt.Sprintf("PR #%d CLOSED", num)
	default:
		return ""
	}
}
