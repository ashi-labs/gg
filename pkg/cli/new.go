package cli

import "github.com/spf13/cobra"

func newNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "new <branch>",
		Aliases: []string{"n"},
		Short:   "Create a new branch + worktree off trunk (regardless of where you are).",
		Args:    cobra.ExactArgs(1),
		RunE:    runNew,
	}
	cmd.Flags().
		BoolP("all", "a", false, "stage all changes in the current worktree and commit them on the new branch")
	cmd.Flags().StringP("message", "m", "", "commit message (required with --all)")
	return cmd
}

func runNew(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	branch := args[0]
	opts, err := newBranchOptsFrom(cmd)
	if err != nil {
		return err
	}
	return appendBranchWorktree(branch, repo.Trunk, opts)
}
