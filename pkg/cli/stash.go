package cli

import (
	"sort"
	"strings"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/spf13/cobra"
)

func newStashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stash",
		Short: "stash uncommitted changes in the current worktree.",
		Long: `wraps ` + "`git stash`" + `. note: stashes are repo-global, not
per-worktree. ` + "`refs/stash`" + ` lives in the bare repo's common dir, so
stashes pushed from one worktree are visible (and pop-able) from
every sibling-branch worktree. label aggressively with -m to keep
the list scannable across stacks.

with no subcommand, behaves like ` + "`gg stash push`" + ` (the most common
flow). subcommands mirror git's: push, pop, list, drop, show.`,
		Args: cobra.ArbitraryArgs,
		RunE: runStashPush,
	}
	addStashPushFlags(cmd)
	cmd.AddCommand(
		newStashPushCmd(),
		newStashPopCmd(),
		newStashListCmd(),
		newStashDropCmd(),
		newStashShowCmd(),
	)
	return cmd
}

func newStashPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push [paths...]",
		Short: "push the current changes onto the stash stack.",
		Args:  cobra.ArbitraryArgs,
		RunE:  runStashPush,
	}
	addStashPushFlags(cmd)
	return cmd
}

func addStashPushFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("message", "m", "", "label the stash entry")
	cmd.Flags().BoolP("include-untracked", "u", false, "also stash untracked files")
	cmd.Flags().Bool("keep-index", false, "leave staged changes in the index after stashing")
}

func runStashPush(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	gitArgs := []string{"stash", "push"}
	if v, _ := cmd.Flags().GetString("message"); v != "" {
		gitArgs = append(gitArgs, "-m", v)
	}
	if v, _ := cmd.Flags().GetBool("include-untracked"); v {
		gitArgs = append(gitArgs, "--include-untracked")
	}
	if v, _ := cmd.Flags().GetBool("keep-index"); v {
		gitArgs = append(gitArgs, "--keep-index")
	}
	if len(args) > 0 {
		gitArgs = append(gitArgs, "--")
		gitArgs = append(gitArgs, args...)
	}
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}

func newStashPopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pop [stash@{n}]",
		Short: "apply the top (or named) stash and drop it from the stack.",
		Args:  cobra.RangeArgs(0, 1),
		RunE:  runStashPop,
	}
}

func runStashPop(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	gitArgs := []string{"stash", "pop"}
	gitArgs = append(gitArgs, args...)
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}

func newStashListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list every stash entry, grouped by branch.",
		Long: `lists every stash in the repo (stashes are repo-global), grouped
by the branch each was pushed from. the current branch's group prints
first; remaining branches print alphabetically. unparseable entries
(stashes pushed from a non-branch context) fall under an "other"
header at the end.`,
		Args: cobra.NoArgs,
		RunE: runStashList,
	}
}

// stashEntry is one parsed line from `git stash list`.
type stashEntry struct {
	selector string // e.g. "stash@{0}"
	branch   string // parsed from the subject; "" if unparseable
	display  string // what we render in the per-branch group
}

func runStashList(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	// `%gd` is the selector (`stash@{N}`); `%gs` is the reflog subject.
	// The pipe separator is safe — neither field can contain a literal
	// `\x1f` byte, but `|` could appear in user-supplied -m messages.
	// Use `\x1f` (ASCII unit separator) for safety.
	raw, err := gitx.In(cwd).Cmd("stash", "list", "--format=%gd\x1f%gs").Bytes()
	if err != nil {
		return err
	}
	body := strings.TrimRight(string(raw), "\n")
	if body == "" {
		return nil
	}
	var entries []stashEntry
	for _, line := range strings.Split(body, "\n") {
		selector, subject, ok := strings.Cut(line, "\x1f")
		if !ok {
			continue
		}
		branch := parseStashBranch(subject)
		entries = append(entries, stashEntry{
			selector: selector,
			branch:   branch,
			display:  selector + ": " + subject,
		})
	}
	current, _ := gitx.Revision.CurrentBranch(cwd)
	groups := groupStashesByBranch(entries, current)
	stdout(renderStashGroups(groups))
	return nil
}

// parseStashBranch pulls the branch name out of a stash subject. Two
// canonical shapes:
//
//   - "WIP on <branch>: <sha> <commit-subject>"  (auto-generated)
//   - "On <branch>: <message>"                   (user passed -m)
//
// Anything else returns "" so the caller can bucket it under "other".
func parseStashBranch(subject string) string {
	for _, prefix := range []string{"WIP on ", "On "} {
		if rest, ok := strings.CutPrefix(subject, prefix); ok {
			if name, _, ok := strings.Cut(rest, ":"); ok {
				return name
			}
		}
	}
	return ""
}

type stashGroup struct {
	branch  string // "" for the catch-all bucket
	entries []stashEntry
}

// groupStashesByBranch buckets entries by branch and orders the
// groups: current branch first, then others alphabetically, then the
// "other" (unparseable-branch) bucket last.
func groupStashesByBranch(entries []stashEntry, current string) []stashGroup {
	byBranch := make(map[string][]stashEntry)
	for _, e := range entries {
		byBranch[e.branch] = append(byBranch[e.branch], e)
	}
	var others []string
	for name := range byBranch {
		if name == "" || name == current {
			continue
		}
		others = append(others, name)
	}
	sort.Strings(others)
	var groups []stashGroup
	if list, ok := byBranch[current]; ok && current != "" {
		groups = append(groups, stashGroup{branch: current, entries: list})
	}
	for _, name := range others {
		groups = append(groups, stashGroup{branch: name, entries: byBranch[name]})
	}
	if list, ok := byBranch[""]; ok {
		groups = append(groups, stashGroup{branch: "", entries: list})
	}
	return groups
}

func renderStashGroups(groups []stashGroup) string {
	var b strings.Builder
	for i, g := range groups {
		if i > 0 {
			b.WriteString("\n")
		}
		header := g.branch
		if header == "" {
			header = "other"
		}
		b.WriteString(style.Stdout.Badge.Render(header))
		b.WriteString("\n")
		for _, e := range g.entries {
			b.WriteString("  ")
			b.WriteString(e.display)
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func newStashDropCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "drop [stash@{n}]",
		Short: "remove the top (or named) stash without applying it.",
		Args:  cobra.RangeArgs(0, 1),
		RunE:  runStashDrop,
	}
}

func runStashDrop(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	gitArgs := []string{"stash", "drop"}
	gitArgs = append(gitArgs, args...)
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}

func newStashShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show [stash@{n}]",
		Short: "show the diff of the top (or named) stash.",
		Args:  cobra.RangeArgs(0, 1),
		RunE:  runStashShow,
	}
	cmd.Flags().BoolP("patch", "p", false, "show full unified diff (default is a stat summary)")
	return cmd
}

func runStashShow(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	gitArgs := []string{"stash", "show"}
	if v, _ := cmd.Flags().GetBool("patch"); v {
		gitArgs = append(gitArgs, "-p")
	}
	gitArgs = append(gitArgs, args...)
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}
