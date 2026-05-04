package cli

import (
	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/spf13/cobra"
)

func newFetchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch [remote...]",
		Short: "fetch refs from a remote without touching local branches.",
		Long: `passes through to ` + "`git fetch`" + `. update remote-tracking refs without
rebasing or merging anything locally — useful for inspecting what's on
origin (` + "`gg log`" + `, ` + "`gg status`" + `) before deciding whether to ` + "`gg sync`" + `.

with --prune, deletes remote-tracking refs whose upstream branch is
gone. with --all, fetches from every configured remote. positional
arguments name specific remotes; default is origin.`,
		Args: cobra.ArbitraryArgs,
		RunE: runFetch,
	}
	cmd.Flags().BoolP("prune", "p", false, "delete remote-tracking refs that no longer exist on the remote")
	cmd.Flags().Bool("all", false, "fetch from every configured remote")
	cmd.Flags().Bool("tags", false, "also fetch tags from the named remotes")
	return cmd
}

func runFetch(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	gitArgs := []string{"fetch"}
	if v, _ := cmd.Flags().GetBool("prune"); v {
		gitArgs = append(gitArgs, "--prune")
	}
	if v, _ := cmd.Flags().GetBool("all"); v {
		gitArgs = append(gitArgs, "--all")
	}
	if v, _ := cmd.Flags().GetBool("tags"); v {
		gitArgs = append(gitArgs, "--tags")
	}
	gitArgs = append(gitArgs, args...)
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}
