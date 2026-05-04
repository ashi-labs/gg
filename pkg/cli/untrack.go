package cli

import (
	"fmt"
	"strings"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/spf13/cobra"
)

func newUntrackCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "untrack [branch]",
		Short:             "drop lineage metadata. leaves branch and worktree alone.",
		Args:              cobra.RangeArgs(0, 1),
		RunE:              runUntrack,
		ValidArgsFunction: completeBranches(compOpts{ExcludeTrunk: true}),
	}
}

func runUntrack(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	var name string
	if len(args) == 1 {
		name = args[0]
	} else {
		current, err := gitx.Revision.CurrentBranch(cwd)
		if err != nil {
			return err
		}
		name = current
	}
	if name == repo.Trunk {
		return fmt.Errorf("cannot untrack trunk")
	}
	b, err := state.LoadBranch(bare, name)
	if err != nil {
		return err
	}
	if b.Parent == "" {
		return fmt.Errorf("%s is not tracked", name)
	}

	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}
	l := stack.Build(repo.Trunk, branches)
	if kids := l.Children(name); len(kids) > 0 {
		return fmt.Errorf(
			"%s has tracked children (%s); untrack them first",
			name,
			strings.Join(kids, ", "),
		)
	}

	if err := state.DeleteBranch(bare, name); err != nil {
		return err
	}
	successf("untracked %s  (branch and worktree kept)", styleBranch(name))
	return nil
}
