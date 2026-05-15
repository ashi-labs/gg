package cli

import (
	"os"

	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/version"
	"github.com/spf13/cobra"
)

const (
	groupCommits    = "commits"
	groupStacks     = "stacks"
	groupNavigation = "navigation"
	groupState      = "state"
	groupRemotes    = "remotes"
	groupConflicts  = "conflicts"
	groupAdmin      = "admin"
)

var (
	cwd              string
	bare             string
	repo             *state.Repo
	ctxResolutionErr error
	debugCommands    []*cobra.Command
	root             = &cobra.Command{
		Use:           "gg",
		Short:         "the gooder cli for stacked-pr + worktree-per-branch git workflows",
		Long:          "gg combines stacked-pr workflows with worktree-per-branch isolation. it draws inspiration from tools like graphite and worktrunk.",
		Version:       version.Build(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cwd, bare, repo, ctxResolutionErr = resolveCtx()
			return nil
		},
	}
)

func init() {
	root.SetHelpFunc(styledHelpFunc)
	root.SetHelpCommand(&cobra.Command{
		Use:   "help [command]",
		Short: "help for any command",
		Run: func(c *cobra.Command, args []string) {
			cmd, _, e := c.Root().Find(args)
			if cmd == nil || e != nil {
				c.Root().Help() //nolint:errcheck
				return
			}
			cmd.InitDefaultHelpFlag()
			cmd.HelpFunc()(cmd, args)
		},
	})
	// hide cobra's auto-injected `completion` subcommand.
	root.CompletionOptions.HiddenDefaultCmd = true
}

type commandGroup struct {
	id, title string
	cmds      []*cobra.Command
}

func commandGroups() []commandGroup {
	return []commandGroup{
		{groupCommits, groupCommits, []*cobra.Command{
			newAddCmd(), newCommitCmd(), newAmendCmd(),
			newStashCmd(), newResetCmd(),
			newRevertCmd(), newRestoreCmd(),
		}},
		{groupStacks, groupStacks, []*cobra.Command{
			newNewCmd(), newAppendCmd(), newFoldCmd(),
			newRenameCmd(), newDeleteCmd(),
			newTrackCmd(), newUntrackCmd(), newRestackCmd(),
			newReparentCmd(),
		}},
		{groupNavigation, groupNavigation, []*cobra.Command{
			newCdCmd(), newCheckoutCmd(),
			newUpstreamCmd(), newDownstreamCmd(),
			newFirstCmd(), newLastCmd(),
			newTrunkCmd(), newReposCmd(),
		}},
		{groupState, groupState, []*cobra.Command{
			newStatusCmd(), newLogCmd(), newDiffCmd(), newBlameCmd(),
		}},
		{groupRemotes, groupRemotes, []*cobra.Command{
			newFetchCmd(), newSyncCmd(), newSubmitCmd(),
		}},
		{groupConflicts, groupConflicts, []*cobra.Command{
			newContinueCmd(), newAbortCmd(),
		}},
		{groupAdmin, groupAdmin, []*cobra.Command{
			newInitCmd(), newCloneCmd(), newLinkCmd(),
			newCleanupCmd(), newConfigCmd(), newShellCmd(), newSkillCmd(), newVersionCmd(),
		}},
	}
}

func Execute() {
	for _, g := range commandGroups() {
		root.AddGroup(&cobra.Group{ID: g.id, Title: g.title})
		for _, c := range g.cmds {
			c.GroupID = g.id
			root.AddCommand(c)
		}
	}
	root.AddCommand(debugCommands...)
	if err := root.Execute(); err != nil {
		errorf("%s", err)
		os.Exit(1)
	}
}
