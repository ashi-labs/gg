package cli

import (
	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/spf13/cobra"
)

func newCommitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "commit [paths...]",
		Aliases: []string{"c"},
		Short:   "commit staged changes in the current worktree.",
		Long: `creates a commit in the current worktree. without -m the configured
editor opens for the commit message.

descendant branches are not restacked: a plain commit appends to the
tip without invalidating the old tip sha, so children stay valid
(merely behind). run ` + "`gg restack`" + ` to propagate the new commit up the
stack — typically after a batch of commits, not after every one.`,
		Args: cobra.ArbitraryArgs,
		RunE: runCommit,
	}
	cmd.Flags().BoolP("all", "a", false, "stage all tracked, modified files before committing")
	cmd.Flags().StringP("message", "m", "", "commit message (skips the editor)")
	cmd.Flags().Bool("no-verify", false, "bypass pre-commit and commit-msg hooks")
	return cmd
}

func runCommit(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	all, _ := cmd.Flags().GetBool("all")
	msg, _ := cmd.Flags().GetString("message")
	noVerify, _ := cmd.Flags().GetBool("no-verify")
	gitArgs := []string{"commit"}
	if all {
		gitArgs = append(gitArgs, "-a")
	}
	if msg != "" {
		gitArgs = append(gitArgs, "-m", msg)
	}
	if noVerify {
		gitArgs = append(gitArgs, "--no-verify")
	}
	if len(args) > 0 {
		gitArgs = append(gitArgs, "--")
		gitArgs = append(gitArgs, args...)
	}
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}
