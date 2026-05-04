package cli

import (
	"fmt"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/registry"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/spf13/cobra"
)

// newCheckoutCmd returns the `gg co` command — a context-aware shortcut
// for `gg cd` scoped to the repo containing the user's current working
// directory. With no arg it hands off to the same picker that `gg cd`
// uses but pre-filtered to that one repo. With a branch name it jumps
// straight to that branch's worktree (matching by exact name against
// trunk or any tracked branch in the current repo).
//
// Fails outside a tracked repo — use `gg cd` for the cross-repo picker.
func newCheckoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "checkout [branch]",
		Aliases: []string{"co"},
		Short:   "Pick a branch in the current repo (repo-scoped `gg cd`).",
		Long: `checkout (co) is a shorthand for ` + "`gg cd`" + ` that only lists branches in the
repo you're currently inside. Useful as a muscle-memory alias for
` + "`git checkout`" + `-style navigation within a repo.

With no arg: opens the picker scoped to this repo.
With a branch name (exactly matching trunk or a tracked branch): jumps
straight to that branch's worktree — no picker.

If the resolved target is the worktree you're already inside, co prints
a "nothing to do" hint instead of a path so the shell wrapper doesn't
redundantly cd to the same spot.

Fails outside a tracked repo — use ` + "`gg cd`" + ` for the cross-repo picker.`,
		Args:              cobra.RangeArgs(0, 1),
		RunE:              runCheckout,
		ValidArgsFunction: completeCheckoutBranches,
	}
}

func runCheckout(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	entries, err := registry.Load()
	if err != nil {
		return err
	}
	var entry *registry.Entry
	for i := range entries {
		if entries[i].Bare == bare {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return fmt.Errorf(
			"current repo isn't registered with gg (run `gg link` from here first)",
		)
	}

	var target string
	if len(args) == 0 {
		// autoSkipUpTo=0 means the picker always shows when there's
		// at least one tracked branch — trunk stays a visible
		// destination instead of getting hidden by auto-skipping to
		// the one other branch.
		target, err = resolveCdTarget([]registry.Entry{*entry}, 0)
	} else {
		// Arg mode: exact-match against trunk or any tracked branch.
		// No fuzzy lookup — that's the picker's job; `gg co <name>`
		// should be a no-surprise direct jump.
		target, err = resolveWorktree(*entry, args[0])
	}
	if err != nil {
		return err
	}
	if target == "" {
		// User cancelled the picker.
		return nil
	}
	return emitCheckoutTarget(*entry, target)
}

// emitCheckoutTarget prints `target` on stdout for the shell wrapper
// to cd into — unless the user is already in that worktree, in which
// case we surface a stderr hint so the "did gg do anything?" question
// has a clear answer. Matters when the user runs `gg co` expecting to
// pick from a set of branches that turns out to be empty or just
// their current spot.
func emitCheckoutTarget(entry registry.Entry, target string) error {
	cwdTop, err := gitx.Revision.TopLevel(cwd)
	if err != nil {
		// Couldn't resolve cwd's worktree — fall back to just
		// emitting the path; shell will cd (no-op or otherwise).
		stdout(target)
		return nil
	}
	if cwdTop != target {
		stdout(target)
		return nil
	}
	// Target equals current worktree — figure out why so the hint is
	// actionable rather than cryptic.
	if trackedBranchCount(entry) == 0 {
		plainln(style.Stderr.Hint.Render(
			"no other branches in " + entry.Name +
				" — run `gg append <name>` to start a stack",
		))
		return nil
	}
	return nil
}

// trackedBranchCount returns the number of non-trunk branches tracked
// by gg in entry. Cheap: one `git config --get-regexp` via
// AllBranches. Used to tailor the "no change" hint.
func trackedBranchCount(entry registry.Entry) int {
	branches, err := state.AllBranches(entry.Bare)
	if err != nil {
		return 0
	}
	n := 0
	for _, b := range branches {
		if b.Name != entry.Trunk {
			n++
		}
	}
	return n
}

// completeCheckoutBranches emits trunk + tracked branch names for the
// current repo. No path completion tier — `gg co` is deliberately
// simpler than `gg cd` and doesn't accept `repo:branch:path` syntax.
func completeCheckoutBranches(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	branches, err := state.AllBranches(bare)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	out := []string{repo.Trunk + "\ttrunk"}
	for _, b := range branches {
		out = append(out, b.Name+"\toff "+b.Parent)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
