package cli

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ashi-labs/gg/pkg/config"
	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/gitx/forge"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

func newLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "log [branch]",
		Aliases: []string{"ls"},
		Short:   "show the stack tree with configurable status columns.",
		Long: `with no argument, renders every stack rooted off trunk. with a branch
argument, narrows to just that stack — its root (the direct child of
trunk that leads to the target), the target itself, and everything
upstream and downstream in the same lineage. trunk is still shown as
the anchor row so the focused stack keeps its visual context.

useful when the full tree is noisy and one stack should be in focus.

with -a/--all, expands each branch with its unique commits
(parent..branch) listed inline under the branch row, newest first.
siblings are sorted by most-recent-commit so the eye lands on hot
work first.`,
		Args:              cobra.RangeArgs(0, 1),
		RunE:              runLog,
		ValidArgsFunction: completeBranches(compOpts{}),
	}
	cmd.Flags().BoolP("all", "a", false, "expand each branch with its unique commits inline")
	// --prefetch: fetch every tracked branch's PR status, write to the on-disk
	// cache, and exit without rendering. Hidden because it's an implementation
	// detail of `gg shell init --prefetch`'s precmd hook, not a user-facing
	// command. Safe to run concurrently with a regular `gg log` thanks to
	// state.json's flock + atomic-rename writes.
	cmd.Flags().Bool("prefetch", false, "warm the PR-status cache and exit (no render)")
	_ = cmd.Flags().MarkHidden("prefetch")
	// --no-cache: skip the on-disk cache for this invocation; useful when
	// you want to force a fresh fetch without waiting for the TTL to lapse.
	cmd.Flags().Bool("no-cache", false, "ignore the on-disk PR-status cache for this run")
	return cmd
}

// logColumn is an optional column to the right of the always-present branch
// name. The render function receives everything it may need so individual
// columns can stay self-contained.
type logColumn struct {
	name   string
	render func(logRowCtx) string
}

// logRowCtx bundles everything a column renderer may need for one row.
type logRowCtx struct {
	row               stackRow
	branch            state.Branch    // populated for non-trunk rows; zero for trunk
	latestCommit      gitx.CommitInfo // zero if this branch has no tip (e.g., unborn trunk)
	latestCommitKnown bool
	prStatus          forge.PRStatus // zero if not fetched or no PR
	prStatusKnown     bool
	spinnerFrame      string // non-empty only in live (TUI) mode while a fetch is in flight
}

// knownLogColumns is the registry of column renderers keyed by config name.
// Defined as a function so per-invocation state (like the cost of dirty
// checks) stays local. ahead-behind splits by row kind — trunk compares to
// origin/<trunk>, branches compare to their parent pointer.
func knownLogColumns() map[string]logColumn {
	return map[string]logColumn{
		"ahead-behind": {
			name: "ahead-behind",
			render: func(c logRowCtx) string {
				if c.row.Branch == repo.Trunk {
					return trunkAheadBehindCell()
				}
				return branchAheadBehindCell(c.branch)
			},
		},
		"age": {
			name: "age",
			render: func(c logRowCtx) string {
				if !c.latestCommitKnown || c.latestCommit.UnixTimestamp == 0 {
					return ""
				}
				return style.Stdout.Dim.Render(compactAge(c.latestCommit.UnixTimestamp))
			},
		},
		"subject": {
			name: "subject",
			render: func(c logRowCtx) string {
				if !c.latestCommitKnown || c.latestCommit.Subject == "" {
					return ""
				}
				return style.Stdout.Dim.Render(truncate(c.latestCommit.Subject, 60))
			},
		},
		"pr": {
			name: "pr",
			render: func(c logRowCtx) string {
				if c.branch.PRNumber <= 0 {
					return ""
				}
				return renderPRCell(c.branch.PRNumber, c.prStatus, c.prStatusKnown, c.spinnerFrame)
			},
		},
		"status": {
			name: "status",
			render: func(c logRowCtx) string {
				// Trunk doesn't get a dirty indicator — users rarely look
				// to `gg log` for trunk worktree state.
				if c.row.Branch == repo.Trunk {
					return ""
				}
				if c.branch.Worktree == "" {
					return style.Stdout.Error.Render("!no-worktree")
				}
				if dirty, _ := gitx.Status.IsDirty(c.branch.Worktree); dirty {
					return style.Stdout.Dirty.Render(style.Glyphs.Dirty + " dirty")
				}
				return ""
			},
		},
	}
}

// resolveLogColumns turns a config-supplied list of column names into the
// ordered list of renderers. Unknown names are dropped with a one-line
// stderr warning so a typo doesn't silently strip columns.
func resolveLogColumns(requested []string) []logColumn {
	known := knownLogColumns()
	out := make([]logColumn, 0, len(requested))
	var unknown []string
	for _, name := range requested {
		if c, ok := known[name]; ok {
			out = append(out, c)
			continue
		}
		unknown = append(unknown, name)
	}
	if len(unknown) > 0 {
		names := make([]string, 0, len(known))
		for n := range known {
			names = append(names, n)
		}
		errorf("gg: unknown log-columns %v — dropping. available: %v\n", unknown, names)
	}
	return out
}

func runLog(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}

	if len(args) == 1 {
		branches, err = filterToStack(repo.Trunk, args[0], branches)
		if err != nil {
			return err
		}
	}

	// Build a name→Branch lookup so column renderers don't each re-query
	// `git config` for the same row.
	branchByName := make(map[string]state.Branch, len(branches))
	names := make([]string, 0, len(branches)+1)
	for _, b := range branches {
		branchByName[b.Name] = b
		names = append(names, b.Name)
	}
	names = append(names, repo.Trunk)

	// One batched git call for subject + committer-date across every branch.
	// Failure here is non-fatal — columns that need tip info just render empty.
	latestCommits, _ := gitx.Ref.LatestCommits(bare, names)

	cfg := config.Load()
	all, _ := cmd.Flags().GetBool("all")
	prefetch, _ := cmd.Flags().GetBool("prefetch")
	noCache, _ := cmd.Flags().GetBool("no-cache")
	columns := resolveLogColumns(cfg.Log.Columns)
	wantsPR := false
	for _, c := range columns {
		if c.name == config.LogColumnPR {
			wantsPR = true
			break
		}
	}

	// --prefetch is the precmd-hook entry point: fetch every branch with a
	// PR, write the cache, exit silently. Skipping render keeps the user's
	// prompt from getting smudged when the hook fires.
	if prefetch {
		return runLogPrefetch(branches, wantsPR)
	}

	// Cache-aware: load the snapshot once (if fresh) and use it as the
	// starting set of PR statuses for the live model. Branches whose status
	// is already in the cache won't trigger a live fetch — the live runner
	// only chases the stragglers. With a precmd hook in place this typically
	// short-circuits the fetch entirely and `gg log` is instant.
	cachedPRStatuses := loadFreshPRStatuses(cfg, noCache)

	// -a expands each branch with its unique commits and sorts siblings by
	// most-recent activity. The recency map is built from the already-fetched
	// LatestCommits (no extra git calls). Commit lines are formatted up front
	// so the tree renderer can pack them into multiline node values.
	var sortByRecency map[string]int64
	var commitLinesByBranch map[string][]string
	if all {
		sortByRecency = make(map[string]int64, len(latestCommits))
		for n, c := range latestCommits {
			sortByRecency[n] = c.UnixTimestamp
		}
		commitLinesByBranch = fetchCommitLines(branches, cfg.Log.CommitsPerBranch, style.Stdout)
	}

	lineage := stack.Build(repo.Trunk, branches)
	current, _ := gitx.Revision.CurrentBranch(cwd)
	rows := renderStackRows(lineage, current, style.Stdout, sortByRecency, commitLinesByBranch)

	// TTY → inline progress view, always (consistent UX). Statuses arrive
	// from the cache instantly when a precmd-hook prefetch has run; any
	// stragglers stream in from live `gh` calls. Non-TTY (piped/CI) takes
	// the synchronous plain path so output stays grep-able.
	if shouldRenderLogLive() {
		return renderLogTableLive(
			rows,
			columns,
			branchByName,
			latestCommits,
			branches,
			cachedPRStatuses,
		)
	}
	prStatusByBranch := fetchPRStatuses(columns, branches)
	for n, s := range cachedPRStatuses {
		if _, ok := prStatusByBranch[n]; !ok {
			if prStatusByBranch == nil {
				prStatusByBranch = map[string]forge.PRStatus{}
			}
			prStatusByBranch[n] = s
		}
	}
	stdout(renderLogTable(rows, columns, branchByName, latestCommits, prStatusByBranch, ""))
	return nil
}

// renderLogTable builds the full log table string from already-resolved
// inputs. Pulled out so the live (bubbletea) path can re-stringify the
// table on every prStatusMsg without duplicating the row/cell wiring.
// spinnerFrame is the current spinner glyph in live mode (empty string
// otherwise); branches that have a PR but no fetched status yet render
// the spinner in the slot where their lifecycle glyph will land.
//
// Per-cell PaddingRight(2) bumps inter-column spacing without going through
// the table's StyleFunc, which the comment below warns is flaky in long
// linear chains. HiddenBorder with outer edges + column borders off keeps
// the table to the cells themselves.
func renderLogTable(
	rows []stackRow,
	columns []logColumn,
	branchByName map[string]state.Branch,
	latestCommits map[string]gitx.CommitInfo,
	prStatusByBranch map[string]forge.PRStatus,
	spinnerFrame string,
) string {
	t := table.New().
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).BorderBottom(false).
		BorderLeft(false).BorderRight(false).
		BorderColumn(false)
	cellPad := style.StdoutRenderer.NewStyle().PaddingRight(2)
	for _, r := range rows {
		cells := make([]string, 0, len(columns)+1)
		cells = append(cells, cellPad.Render(r.Label))
		// Commit sub-rows only carry their own label in the leftmost cell —
		// the column slots stay blank so per-branch columns line up against
		// the branch header above, not against arbitrary commit lines.
		if r.IsCommit {
			for range columns {
				cells = append(cells, "")
			}
			t.Row(cells...)
			continue
		}
		rc := logRowCtx{row: r}
		if r.Branch != repo.Trunk {
			rc.branch = branchByName[r.Branch]
		}
		if latestCommits != nil {
			if commit, ok := latestCommits[r.Branch]; ok {
				rc.latestCommit = commit
				rc.latestCommitKnown = true
			}
		}
		if s, ok := prStatusByBranch[r.Branch]; ok {
			rc.prStatus = s
			rc.prStatusKnown = true
		}
		rc.spinnerFrame = spinnerFrame
		for _, col := range columns {
			cells = append(cells, cellPad.Render(col.render(rc)))
		}
		t.Row(cells...)
	}
	return t.String()
}

// filterToStack returns the subset of branches that belong to focus's stack:
// the stack root (the direct child of trunk that leads to focus) together
// with every descendant of that root. Errors when focus is trunk or not
// tracked — both would produce a confusing empty render otherwise.
func filterToStack(trunk, focus string, branches []state.Branch) ([]state.Branch, error) {
	if focus == trunk {
		return nil, fmt.Errorf("%s is trunk — omit the argument to render the full tree", focus)
	}
	l := stack.Build(trunk, branches)
	if !l.Contains(focus) {
		return nil, fmt.Errorf("%s is not a tracked branch", focus)
	}
	keep := make(map[string]bool, len(branches))
	for _, n := range l.StackOf(focus) {
		keep[n] = true
	}
	out := make([]state.Branch, 0, len(keep))
	for _, b := range branches {
		if keep[b.Name] {
			out = append(out, b)
		}
	}
	return out, nil
}

// loadFreshPRStatuses reads the on-disk PR-status cache and returns the
// entries that are still within the configured TTL, keyed by branch name.
// Callers use this map to skip live fetches for already-known statuses.
//
// noCache (--no-cache) is a per-invocation override that pretends the
// cache is empty — useful when the user wants to force a fresh fetch
// without waiting for the TTL to lapse. A negative or zero TTL in config
// also disables the cache wholesale.
func loadFreshPRStatuses(cfg config.Config, noCache bool) map[string]forge.PRStatus {
	if noCache || cfg.PR.CacheTTLSeconds <= 0 || bare == "" {
		return nil
	}
	c, err := state.LoadPRCache(bare)
	if err != nil {
		return nil
	}
	ttl := time.Duration(cfg.PR.CacheTTLSeconds) * time.Second
	if !c.IsFresh(ttl) {
		return nil
	}
	out := make(map[string]forge.PRStatus, len(c.By))
	for name, e := range c.By {
		out[name] = forge.PRStatus{State: e.State, IsDraft: e.IsDraft}
	}
	return out
}

// writePRCacheFromModel persists the live model's resolved statuses back
// into state.json. Best-effort — a write failure just means the next
// invocation will re-fetch, no user-visible breakage.
//
// We only persist branches whose entry actually has a State (i.e., a fetch
// resolved successfully). Branches whose fetch errored mid-flight are
// dropped from the cache so they're retried next time, rather than being
// pinned to a stale value.
func writePRCacheFromModel(branches []state.Branch, statuses map[string]forge.PRStatus) {
	if bare == "" || len(statuses) == 0 {
		return
	}
	prByName := make(map[string]int, len(branches))
	for _, b := range branches {
		if b.PRNumber > 0 {
			prByName[b.Name] = b.PRNumber
		}
	}
	by := make(map[string]state.PRStatusEntry, len(statuses))
	for name, s := range statuses {
		num, ok := prByName[name]
		if !ok || s.State == "" {
			continue
		}
		by[name] = state.PRStatusEntry{PR: num, State: s.State, IsDraft: s.IsDraft}
	}
	if len(by) == 0 {
		return
	}
	_ = state.SavePRCache(bare, by)
}

// runLogPrefetch is the entry point for `gg log --prefetch`. It fetches
// every tracked branch's PR status in parallel, writes the result into
// the on-disk cache, and exits — no rendering. Designed to be invoked
// from the shell wrapper's precmd hook in the background, where the
// user's prompt is mid-redraw and any stdout/stderr output would smudge
// it. Any error short of a hard failure is swallowed; the contract is
// "best-effort cache warm, never break the user's prompt."
//
// Bails out cleanly when there's nothing to do (no `pr` column wanted,
// no forge configured, or no tracked branches with PRs) so the precmd
// hook can fire indiscriminately and pay only the cost of process
// startup when the cache wouldn't help anyway.
func runLogPrefetch(branches []state.Branch, wantsPR bool) error {
	if !wantsPR || gitx.Forge == nil || bare == "" {
		return nil
	}
	// Per-entry storm guard: only MERGED is terminal (PRs can't be
	// un-merged), so we carry MERGED entries straight through and only
	// fetch branches whose cached state is non-terminal-or-missing. The
	// previous "if cache fresh, skip wholesale" guard pinned OPEN entries
	// for the full TTL after a merge landed remotely — minutes of stale
	// data in a tool whose precmd hook exists specifically to avoid that.
	prior, _ := state.LoadPRCache(bare)
	by := make(map[string]state.PRStatusEntry)
	type result struct {
		name   string
		status forge.PRStatus
		num    int
	}
	ch := make(chan result, len(branches))
	var wg sync.WaitGroup
	for _, b := range branches {
		if b.PRNumber <= 0 || b.Worktree == "" {
			continue
		}
		if e, ok := prior.By[b.Name]; ok && e.State == "MERGED" && e.PR == b.PRNumber {
			by[b.Name] = e
			continue
		}
		wg.Add(1)
		go func(b state.Branch) {
			defer wg.Done()
			s, err := gitx.Forge.GetPRStatus(b.Worktree, b.PRNumber)
			if err != nil {
				return
			}
			ch <- result{name: b.Name, status: s, num: b.PRNumber}
		}(b)
	}
	wg.Wait()
	close(ch)
	for r := range ch {
		if r.status.State == "" {
			continue
		}
		by[r.name] = state.PRStatusEntry{
			PR:      r.num,
			State:   r.status.State,
			IsDraft: r.status.IsDraft,
		}
	}
	// Even an empty `by` is worth writing — it stamps UpdatedAt so the next
	// `gg log` knows "we just looked, there genuinely are no PRs to show"
	// rather than re-running the same empty fetch loop.
	return state.SavePRCache(bare, by)
}

// fetchPRStatuses queries the forge for every branch with a PR, in parallel,
// and returns a name→state map. Returns nil when the column isn't requested,
// the forge isn't configured, or no branches carry PR numbers — callers can
// safely index into a nil map. Per-branch errors render the cell empty
// rather than failing the whole log; we surface a single stderr line per
// failure so transient outages don't go fully silent.
func fetchPRStatuses(columns []logColumn, branches []state.Branch) map[string]forge.PRStatus {
	wanted := false
	for _, c := range columns {
		if c.name == config.LogColumnPR {
			wanted = true
			break
		}
	}
	if !wanted || gitx.Forge == nil {
		return nil
	}
	type result struct {
		name   string
		status forge.PRStatus
	}
	ch := make(chan result, len(branches))
	var wg sync.WaitGroup
	for _, b := range branches {
		if b.PRNumber <= 0 || b.Worktree == "" {
			continue
		}
		wg.Add(1)
		go func(b state.Branch) {
			defer wg.Done()
			s, err := gitx.Forge.GetPRStatus(b.Worktree, b.PRNumber)
			if err != nil {
				errorf("checking PR #%d for %s: %v", b.PRNumber, styleBranch(b.Name), err)
				return
			}
			ch <- result{name: b.Name, status: s}
		}(b)
	}
	wg.Wait()
	close(ch)
	out := make(map[string]forge.PRStatus)
	for r := range ch {
		out[r.name] = r.status
	}
	return out
}

// renderPRCell renders the unified `pr` column: a lifecycle glyph followed by
// `#<num>`, both colored by the PR's forge state so the number and indicator
// read as one signal. While the live path is still waiting on a fetch we
// render `<spinner> #<num>` in dim — the spinner occupies the same slot the
// lifecycle glyph will land in, which keeps the cell width stable across
// re-renders (lipgloss's table drops rows whose cell width changes between
// frames). Synchronous callers pass spinnerFrame="" and fall back to the
// historical plain `#<num>` for the no-status case.
func renderPRCell(num int, s forge.PRStatus, known bool, spinnerFrame string) string {
	body := fmt.Sprintf("#%d", num)
	if !known {
		if spinnerFrame != "" {
			return style.Stdout.Dim.Render(spinnerFrame + " " + body)
		}
		return style.Stdout.Hint.Render("  " + body)
	}
	g := style.Glyphs.PRGlyphs
	switch s.State {
	case "OPEN":
		if s.IsDraft {
			return style.Stdout.Dim.Render(g.Draft + " " + body)
		}
		return style.Stdout.Success.Render(g.Open + " " + body)
	case "MERGED":
		return style.Stdout.Current.Render(g.Merged + " " + body)
	case "CLOSED":
		return style.Stdout.Error.Render(g.Closed + " " + body)
	default:
		return style.Stdout.Hint.Render("  " + body)
	}
}

// trunkAheadBehindCell compares trunk to origin/<trunk>. No origin or
// comparison failure renders empty — callers never surface git's error to
// the user; the cell just goes blank.
func trunkAheadBehindCell() string {
	if !gitx.Remote.Exists(bare, "origin") {
		return ""
	}
	ahead, behind, err := gitx.Ref.AheadBehind(
		repo.PrimaryWorktree,
		repo.Trunk,
		"origin/"+repo.Trunk,
	)
	if err != nil {
		return ""
	}
	return joinStatusParts(formatAheadBehind(ahead, behind))
}

func branchAheadBehindCell(b state.Branch) string {
	if b.Parent == "" || b.Worktree == "" {
		return ""
	}
	ahead, behind, err := gitx.Ref.AheadBehind(b.Worktree, b.Name, b.Parent)
	if err != nil {
		return ""
	}
	return joinStatusParts(formatAheadBehind(ahead, behind))
}

func formatAheadBehind(ahead, behind int) []string {
	var out []string
	if ahead > 0 {
		out = append(out, style.Stdout.Dim.Render(fmt.Sprintf("↑%d", ahead)))
	}
	if behind > 0 {
		out = append(out, style.Stdout.Dirty.Render(fmt.Sprintf("↓%d", behind)))
	}
	return out
}

func joinStatusParts(parts []string) string {
	return strings.Join(parts, "  ")
}

// compactAge renders a committer-date unix timestamp as a short
// human-readable span — "3d", "2h", "now" — tuned for the log column.
func compactAge(unix int64) string {
	if unix == 0 {
		return ""
	}
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < 90*time.Second:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

// fetchCommitLines fans out one `git log parent..branch` per tracked branch
// and returns pre-formatted, pre-styled commit lines ready for the tree
// renderer to embed as multiline node values. cap is the per-branch limit;
// 0 means "no cap". When the commit count exceeds cap, an "… +N more"
// overflow line is appended in the hint style.
//
// Errors per branch are swallowed (with a one-line stderr note) — a single
// empty-due-to-error branch shouldn't kill the whole timeline. Branches
// with no parent or no worktree (e.g., trunk, or a state row that lost its
// worktree pointer) are skipped silently.
func fetchCommitLines(branches []state.Branch, cap int, pal style.Palette) map[string][]string {
	type result struct {
		name  string
		lines []string
	}
	ch := make(chan result, len(branches))
	var wg sync.WaitGroup
	// Fetch one extra commit when capped so we can detect overflow without
	// a second call. Pass 0 through unchanged (no cap).
	fetchLimit := 0
	if cap > 0 {
		fetchLimit = cap + 1
	}
	for _, b := range branches {
		if b.Parent == "" || b.Worktree == "" {
			continue
		}
		wg.Add(1)
		go func(b state.Branch) {
			defer wg.Done()
			commits, err := gitx.Ref.UniqueCommits(b.Worktree, b.Parent, b.Name, fetchLimit)
			if err != nil {
				errorf("listing commits for %s: %v", styleBranch(b.Name), err)
				return
			}
			if len(commits) == 0 {
				return
			}
			overflow := 0
			if cap > 0 && len(commits) > cap {
				overflow = totalCommitsBeyondCap(b.Worktree, b.Parent, b.Name, cap)
				commits = commits[:cap]
			}
			lines := make([]string, 0, len(commits)+1)
			for _, c := range commits {
				lines = append(lines, formatCommitLine(c, pal))
			}
			if overflow > 0 {
				lines = append(lines, pal.Dim.Render(fmt.Sprintf("… +%d more", overflow)))
			}
			ch <- result{name: b.Name, lines: lines}
		}(b)
	}
	wg.Wait()
	close(ch)
	out := make(map[string][]string)
	for r := range ch {
		out[r.name] = r.lines
	}
	return out
}

// totalCommitsBeyondCap counts the exact number of commits past the cap so
// the overflow line ("… +N more") shows the real remainder rather than just
// "+1 more." On error, returns 1 — we already know at least cap+1 commits
// exist (the fetcher saw them), so flag overflow even if the count call
// itself fails.
func totalCommitsBeyondCap(dir, parent, branch string, cap int) int {
	total, err := gitx.Ref.CountCommits(dir, parent, branch)
	if err != nil {
		return 1
	}
	if total <= cap {
		return 0
	}
	return total - cap
}

// formatCommitLine renders one commit as `<sha>  <age>  <subject>` in dim,
// with fixed-width SHA / age fields and a subject capped at 40 visible runes
// so each commit line has a deterministic upper-bound width (independent of
// what the commit author wrote). All three fields are dim since commit lines
// are contextual info under the (already-styled) branch header.
func formatCommitLine(c gitx.CommitInfo, pal style.Palette) string {
	sha := pal.Dim.Render(c.ShortSHA)
	age := pal.Dim.Render(fmt.Sprintf("%-9s", compactAge(c.UnixTimestamp)))
	subj := pal.Dim.Render(truncate(c.Subject, 40))
	return sha + "  " + age + "  " + subj
}

// truncate shortens a string to at most n visible runes, appending an
// ellipsis when it trims. Works on runes to avoid cutting multibyte chars.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
