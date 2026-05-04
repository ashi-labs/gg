package cli

import (
	"fmt"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/spf13/cobra"
)

func newRmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm [paths...]",
		Short: "remove files (default) or a branch with -b.",
		Long: `wraps ` + "`git rm`" + ` for files. with -b/--branch, delegates to
` + "`gg delete`" + ` to remove a branch and its worktree (with the same
flags: -r, -y).

defaults to file mode so the muscle-memory of unix ` + "`rm`" + ` and ` + "`git rm`" + `
keeps working. -b is the explicit opt-in for branch operations,
which avoids the silent-misclassification footgun of trying to guess
whether an argument names a path or a branch.`,
		Args:              cobra.MinimumNArgs(1),
		RunE:              runRm,
		ValidArgsFunction: completeRmArgs,
	}
	cmd.Flags().BoolP("branch", "b", false, "delete a branch instead of files (delegates to `gg delete`)")
	cmd.Flags().BoolP("recursive", "r", false, "recursive — for files: descend into dirs; for branches: with -b, delete the subtree")
	cmd.Flags().BoolP("force", "f", false, "force — for files: override `git rm`'s safety checks")
	cmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt (branch mode only)")
	cmd.Flags().Bool("cached", false, "drop from the index only; leave the working-tree file alone")
	return cmd
}

func runRm(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	if branch, _ := cmd.Flags().GetBool("branch"); branch {
		return runRmBranch(cmd, args)
	}
	gitArgs := []string{"rm"}
	if v, _ := cmd.Flags().GetBool("recursive"); v {
		gitArgs = append(gitArgs, "-r")
	}
	if v, _ := cmd.Flags().GetBool("force"); v {
		gitArgs = append(gitArgs, "-f")
	}
	if v, _ := cmd.Flags().GetBool("cached"); v {
		gitArgs = append(gitArgs, "--cached")
	}
	gitArgs = append(gitArgs, "--")
	gitArgs = append(gitArgs, args...)
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}

// completeRmArgs routes by mode: with -b, suggest tracked branches
// (excluding the current one — same as `gg delete`'s completer);
// otherwise suggest tracked files (`git rm` only accepts paths that
// are committed in HEAD or staged, so dirty/untracked candidates are
// nonsense here).
func completeRmArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if branch, _ := cmd.Flags().GetBool("branch"); branch {
		return completeBranches(compOpts{ExcludeCurrent: true})(cmd, args, toComplete)
	}
	return completeTrackedPaths(cmd, args, toComplete)
}

// runRmBranch hands off to the same code path as `gg delete` so the
// branch-mode of `gg rm -b <name>` and `gg delete <name>` produce
// identical behavior. -r maps to --recursive; -y to --yes.
func runRmBranch(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("`gg rm -b` takes exactly one branch name")
	}
	delete := newDeleteCmd()
	delete.SetArgs(args)
	if v, _ := cmd.Flags().GetBool("recursive"); v {
		_ = delete.Flags().Set("recursive", "true")
	}
	if v, _ := cmd.Flags().GetBool("yes"); v {
		_ = delete.Flags().Set("yes", "true")
	}
	return delete.RunE(delete, args)
}
