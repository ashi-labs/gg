package cli

import (
	"fmt"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/spf13/cobra"
)

func newMvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mv <src> <dst>",
		Short: "move/rename files (default) or a branch with -b.",
		Long: `wraps ` + "`git mv`" + ` for files. with -b/--branch, renames a branch
and its worktree:

  gg mv -b <new>            rename the current branch (delegates to gg rename)
  gg mv -b <old> <new>      rename <old> to <new>

defaults to file mode so the muscle-memory of unix ` + "`mv`" + ` and ` + "`git mv`" + `
keeps working. -b is the explicit opt-in for branch operations.`,
		Args:              cobra.MinimumNArgs(1),
		RunE:              runMv,
		ValidArgsFunction: completeMvArgs,
	}
	cmd.Flags().BoolP("branch", "b", false, "rename a branch instead of files (delegates to `gg rename`)")
	cmd.Flags().BoolP("force", "f", false, "overwrite the destination if it exists (file mode)")
	return cmd
}

func runMv(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	if branch, _ := cmd.Flags().GetBool("branch"); branch {
		return runMvBranch(cmd, args)
	}
	if len(args) < 2 {
		return fmt.Errorf("`gg mv` takes at least <src> and <dst>")
	}
	gitArgs := []string{"mv"}
	if v, _ := cmd.Flags().GetBool("force"); v {
		gitArgs = append(gitArgs, "-f")
	}
	gitArgs = append(gitArgs, args...)
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}

// runMvBranch routes to `gg rename` for the simple case (one arg =
// rename current) and to a direct ref-rename for the explicit-source
// case (two args = rename <old> to <new>). Two-arg form mirrors
// `git branch -m <old> <new>`.
func runMvBranch(cmd *cobra.Command, args []string) error {
	switch len(args) {
	case 1:
		rename := newRenameCmd()
		rename.SetArgs(args)
		return rename.RunE(rename, args)
	case 2:
		current, err := gitx.Revision.CurrentBranch(cwd)
		if err != nil {
			return err
		}
		// Two-arg form on the current branch is just the one-arg form.
		// Forward to gg rename so worktree-dir + state metadata + open-PR
		// retargeting all stay consistent. Renaming a non-current branch
		// is rejected up front because rename() rewrites the worktree
		// directory, which only works when the user is standing in it.
		if args[0] != current {
			return fmt.Errorf(
				"can only rename the current branch; cd into %s first or use `gg rename`",
				args[0],
			)
		}
		rename := newRenameCmd()
		rename.SetArgs(args[1:])
		return rename.RunE(rename, args[1:])
	default:
		return fmt.Errorf(
			"`gg mv -b` takes either <new> (rename current) or <old> <new>",
		)
	}
}

// completeMvArgs routes by mode:
//   - file mode (default): suggest tracked paths at position 0 (source
//     must be committed; `git mv` rejects untracked sources). Position
//     1+ is a freeform destination, so no suggestions.
//   - branch mode (-b): suggest tracked branches excluding the current
//     one at position 0. Position 1 is a freeform new name.
func completeMvArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if branch, _ := cmd.Flags().GetBool("branch"); branch {
		if len(args) == 0 {
			return completeBranches(compOpts{ExcludeCurrent: true})(cmd, args, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if len(args) == 0 {
		return completeTrackedPaths(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}
