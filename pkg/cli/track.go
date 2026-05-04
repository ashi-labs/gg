package cli

import (
	"fmt"
	"path/filepath"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/spf13/cobra"
)

func newTrackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "track",
		Short: "Adopt the current branch into a stack.",
		Long: `Registers the current branch in gg's lineage. Run this from the worktree
of a branch that exists but wasn't created with gg append/new.`,
		Args: cobra.NoArgs,
		RunE: runTrack,
	}
	cmd.Flags().String("parent", "", "parent branch (default: trunk)")
	_ = cmd.RegisterFlagCompletionFunc("parent", completeBranches(compOpts{IncludeTrunk: true}))
	return cmd
}

func runTrack(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	current, err := gitx.Revision.CurrentBranch(cwd)
	if err != nil {
		return err
	}
	if current == repo.Trunk {
		return fmt.Errorf("cannot track trunk (%s)", repo.Trunk)
	}
	if current == "HEAD" || current == "" {
		return fmt.Errorf("detached HEAD; checkout a branch first")
	}

	existing, err := state.LoadBranch(bare, current)
	if err != nil {
		return err
	}
	if existing.Parent != "" {
		return fmt.Errorf(
			"%s is already tracked (parent: %s); use `gg untrack` first to change",
			current,
			existing.Parent,
		)
	}

	parent, _ := cmd.Flags().GetString("parent")
	if parent == "" {
		parent = repo.Trunk
	}
	parentSHA, err := gitx.Revision.HeadSHA(bare, parent)
	if err != nil {
		return fmt.Errorf("parent %q: %w", parent, err)
	}

	top, err := gitx.Revision.TopLevel(cwd)
	if err != nil {
		return err
	}
	b := state.Branch{
		Name:      current,
		Parent:    parent,
		ParentSHA: parentSHA,
		Worktree:  filepath.Clean(top),
	}
	if err := state.SaveBranch(bare, b); err != nil {
		return err
	}
	successf("tracking %s  (parent: %s)", styleBranch(current), styleBranch(parent))
	return nil
}
