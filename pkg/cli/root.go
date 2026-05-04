package cli

import (
	"os"

	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/version"
	"github.com/spf13/cobra"
)

var (
	cwd              string
	bare             string
	repo             *state.Repo
	ctxResolutionErr error
	root             = &cobra.Command{
		Use:           "gg",
		Short:         "Stacked PRs meet worktrees.",
		Long:          "gg combines stacked-PR workflows (git-town, charcoal) with worktree-per-branch isolation (worktrunk).",
		Version:       version.Build(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cwd, bare, repo, ctxResolutionErr = resolveCtx()
			return nil
		},
	}
	debugCommands []*cobra.Command
)

func init() {
	root.SetHelpFunc(styledHelpFunc)
	// Hide cobra's auto-injected `completion` subcommand. Completion
	// scripts are still reachable via the GenZshCompletion / GenBashCompletion
	// / GenFishCompletion methods that `gg shell install` calls — we just
	// don't want a parallel user-facing surface to confuse "how do I install".
	root.CompletionOptions.HiddenDefaultCmd = true
}

func Execute() {
	root.AddCommand(
		newInitCmd(),
		newCloneCmd(),
		newAddCmd(),
		newAppendCmd(),
		newCommitCmd(),
		newNewCmd(),
		newCheckoutCmd(),
		newLogCmd(),
		newTrackCmd(),
		newUntrackCmd(),
		newRenameCmd(),
		newDeleteCmd(),
		newFoldCmd(),
		newUpstreamCmd(),
		newDownstreamCmd(),
		newFirstCmd(),
		newLastCmd(),
		newTrunkCmd(),
		newSyncCmd(),
		newRestackCmd(),
		newContinueCmd(),
		newAbortCmd(),
		newSubmitCmd(),
		newReposCmd(),
		newCdCmd(),
		newCleanupCmd(),
		newLinkCmd(),
		newShellCmd(),
		newVersionCmd(),
	)
	root.AddCommand(debugCommands...)
	if err := root.Execute(); err != nil {
		errorf("%s", err)
		os.Exit(1)
	}
}
