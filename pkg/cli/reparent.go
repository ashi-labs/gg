package cli

import (
	"fmt"
	"slices"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/sync"
	"github.com/spf13/cobra"
)

func newReparentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "reparent [<branch>] <new-parent>",
		Aliases: []string{"rp"},
		Short:   "replay a branch's commits onto a different parent.",
		Long: `move a branch (and, by default, its descendants) so it stacks on
top of <new-parent> instead of its current parent. the branch's own
commits are replayed onto the new base via ` + "`git rebase --onto`" + `, then
any cascading rebases finish in the same restack pass.

forms:
  gg reparent <new-parent>            move the current branch
  gg reparent <branch> <new-parent>   move the named branch

with --pick the named branch is moved alone: its children are
re-pointed to its old parent and have its commits dropped from
their history on the next restack.`,
		Args:              cobra.RangeArgs(1, 2),
		RunE:              runReparent,
		ValidArgsFunction: completeReparentArgs,
	}
	cmd.Flags().Bool("pick", false, "move only the named branch; reparent its children onto its old parent")
	return cmd
}

func completeReparentArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) >= 2 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	// Both positions accept any tracked branch (or trunk for the
	// new-parent slot). Source-position completions exclude trunk via
	// runReparent's validation, but we leave it visible here so the
	// user gets a clear refusal rather than a missing entry.
	return completeBranches(compOpts{IncludeTrunk: true})(cmd, args, toComplete)
}

func runReparent(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	pick, _ := cmd.Flags().GetBool("pick")

	var source, newParent string
	switch len(args) {
	case 1:
		current, err := gitx.Revision.CurrentBranch(cwd)
		if err != nil {
			return err
		}
		if current == "HEAD" || current == "" {
			return fmt.Errorf("detached HEAD; pass <branch> explicitly or `gg cd` first")
		}
		source = current
		newParent = args[0]
	case 2:
		source = args[0]
		newParent = args[1]
	}

	if source == repo.Trunk {
		return fmt.Errorf("cannot reparent trunk (%s)", repo.Trunk)
	}
	if source == newParent {
		return fmt.Errorf("%s is already its own parent — nothing to do", source)
	}

	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}
	l := stack.Build(repo.Trunk, branches)
	if !l.Contains(source) {
		return fmt.Errorf("%s is not tracked (run `gg track` first)", source)
	}
	if !l.Contains(newParent) {
		return fmt.Errorf("%s is not tracked (use trunk %s or run `gg track`)", newParent, repo.Trunk)
	}

	srcBranch, err := state.LoadBranch(bare, source)
	if err != nil {
		return err
	}
	oldParent := srcBranch.Parent
	if oldParent == "" {
		return fmt.Errorf("%s has no recorded parent", source)
	}
	if oldParent == newParent {
		return fmt.Errorf("%s is already parented on %s", source, newParent)
	}

	// Cycle check: refuse if new-parent is source itself or anywhere
	// inside source's subtree. Without this we'd happily commit a
	// circular Parent chain that no rebase plan can resolve.
	if slices.Contains(l.Descendants(source), newParent) {
		return fmt.Errorf(
			"cycle: %s is a descendant of %s; reparent %s onto an outside branch first",
			newParent, source, newParent,
		)
	}

	oldParentSHA, err := gitx.Revision.HeadSHA(bare, oldParent)
	if err != nil {
		return fmt.Errorf("resolving old parent %s: %w", oldParent, err)
	}

	if pick {
		// Re-point each direct child of source onto source's old parent
		// and pin its parent-sha to source's CURRENT tip — that's the
		// boundary the next rebase will use to drop source's commits
		// out of the child's history (`git rebase --onto oldParent
		// sourceTip`). Capture sourceTip BEFORE the source rebase
		// changes it.
		sourceTip, err := gitx.Revision.HeadSHA(bare, source)
		if err != nil {
			return fmt.Errorf("resolving %s tip: %w", source, err)
		}
		for _, kid := range l.Children(source) {
			if err := state.UpdateParent(bare, kid, oldParent); err != nil {
				return err
			}
			if err := state.UpdateParentSHA(bare, kid, sourceTip); err != nil {
				return err
			}
		}
	}

	// Move source itself: change recorded parent, but seed parent-sha
	// with the OLD parent's tip so the engine sees a parent move
	// (parentNow=newParent.HEAD, parentOld=oldParent.HEAD) and replays
	// only source's own commits onto newParent.
	if err := state.UpdateParent(bare, source, newParent); err != nil {
		return err
	}
	if err := state.UpdateParentSHA(bare, source, oldParentSHA); err != nil {
		return err
	}

	// Run a whole-repo restack (NoFetch). Only touched stacks actually
	// rebase — every other branch hits the parentNow==parentOld skip
	// path. Whole-repo scope (not stack-scoped) is needed for --pick,
	// where reparented children land in a different stack from source.
	title := fmt.Sprintf("reparenting %s onto %s", source, newParent)
	if err := runSyncWithProgress(sync.RunOpts{NoFetch: true}, title, nil); err != nil {
		return err
	}
	if pick {
		successf("reparented %s onto %s (children left on %s)",
			styleBranch(source), styleBranch(newParent), styleBranch(oldParent))
	} else {
		successf("reparented %s onto %s", styleBranch(source), styleBranch(newParent))
	}
	// Refresh PR footers so any open PRs whose stack-tree text shifted
	// reflect the new shape. Best-effort, same gating as `gg sync`.
	_ = refreshOpenPRFooters()
	return nil
}
