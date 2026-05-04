package cli

import (
	"fmt"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/spf13/cobra"
)

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <paths...>",
		Short: "discard working-tree changes (or unstage them with --staged).",
		Long: `wraps ` + "`git restore`" + `. with paths and no flags, throws away
working-tree edits to those paths and brings them back to the index
(or head if nothing's staged). irreversible — there's no reflog for
working-tree state.

with --staged, leaves the working tree alone and unstages the named
paths instead (the inverse of ` + "`gg add`" + `).

with --source=<ref>, restores the paths' contents to whatever they
looked like at <ref> (a branch, tag, or sha). useful for "give me
this file the way it was on main".`,
		Args:              cobra.MinimumNArgs(1),
		RunE:              runRestore,
		ValidArgsFunction: completeRestoreArgs,
	}
	cmd.Flags().Bool("staged", false, "unstage paths (leave the working tree alone)")
	cmd.Flags().String("source", "", "restore paths from this ref instead of head/index")
	return cmd
}

func runRestore(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	if len(args) == 0 {
		return fmt.Errorf("nothing to restore — pass at least one path")
	}
	gitArgs := []string{"restore"}
	if v, _ := cmd.Flags().GetBool("staged"); v {
		gitArgs = append(gitArgs, "--staged")
	}
	if src, _ := cmd.Flags().GetString("source"); src != "" {
		gitArgs = append(gitArgs, "--source", src)
	}
	gitArgs = append(gitArgs, "--")
	gitArgs = append(gitArgs, args...)
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}

// completeRestoreArgs picks the candidate set by mode:
//   - --source=<ref>: any tracked path (the user is rewriting from a
//     different ref, so even clean paths are valid targets).
//   - default / --staged: dirty paths (the only paths where a no-source
//     restore does anything useful — clean paths are no-ops).
func completeRestoreArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if src, _ := cmd.Flags().GetString("source"); src != "" {
		return completeTrackedPaths(cmd, args, toComplete)
	}
	return completeStageablePaths(cmd, args, toComplete)
}
