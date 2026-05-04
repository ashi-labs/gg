package cli

import (
	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/spf13/cobra"
)

func newCommitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "commit [paths...]",
		Aliases: []string{"c"},
		Short:   "Commit staged changes in the current worktree.",
		Long: `commit (c) creates a commit in the current worktree. With -m the
message is taken from the flag and the editor is skipped; without -m
your editor opens as usual. -a stages every tracked file that's been
modified or deleted before committing (untracked files are ignored —
use ` + "`gg add`" + ` for those).`,
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
