package cli

import (
	"fmt"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/spf13/cobra"
)

func newRevertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revert <commit>...",
		Short: "create a new commit that undoes the changes from the named commit(s).",
		Long: `wraps ` + "`git revert`" + `. for each named commit, computes the inverse
patch and applies it as a new commit on top of the current branch.
unlike ` + "`gg reset`" + `, history isn't rewritten — the original commit stays
in place and a new commit lands beside it.

without --no-edit the configured editor opens to confirm or amend the
auto-generated revert message.

descendant branches are not restacked: revert appends to the tip,
leaving the old tip intact in the graph, so children stay valid (just
behind by one commit). run ` + "`gg restack`" + ` to propagate the revert up
the stack.`,
		Args:              cobra.MinimumNArgs(1),
		RunE:              runRevert,
		ValidArgsFunction: completeRevertArgs,
	}
	cmd.Flags().Bool("no-edit", false, "skip the editor; use the auto-generated revert message")
	cmd.Flags().Bool("no-verify", false, "bypass pre-commit and commit-msg hooks")
	return cmd
}

func runRevert(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	if len(args) == 0 {
		return fmt.Errorf("nothing to revert — pass at least one commit-ish")
	}
	gitArgs := []string{"revert"}
	if v, _ := cmd.Flags().GetBool("no-edit"); v {
		gitArgs = append(gitArgs, "--no-edit")
	}
	if v, _ := cmd.Flags().GetBool("no-verify"); v {
		gitArgs = append(gitArgs, "--no-verify")
	}
	gitArgs = append(gitArgs, args...)
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}

// completeRevertArgs offers commit-ish targets at every position —
// `git revert` accepts one or more commit-ishes, never paths. Reuses
// the same helper `gg reset` uses so the two commands stay in sync.
func completeRevertArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if repo == nil {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return commitishCompletions(), cobra.ShellCompDirectiveNoFileComp
}
