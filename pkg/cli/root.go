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
	debugCommands []*cobra.Command
)

func init() {
	root.SetHelpFunc(styledHelpFunc)
	// Override cobra's auto-injected `help` command so its Short matches
	// the lowercase house style. SetHelpCommand also disables cobra's
	// default help command and replaces it with this one.
	root.SetHelpCommand(&cobra.Command{
		Use:   "help [command]",
		Short: "help for any command",
		Run: func(c *cobra.Command, args []string) {
			cmd, _, e := c.Root().Find(args)
			if cmd == nil || e != nil {
				c.Root().Help() //nolint:errcheck
				return
			}
			cmd.InitDefaultHelpFlag() //nolint:errcheck
			cmd.HelpFunc()(cmd, args)
		},
	})
	// Hide cobra's auto-injected `completion` subcommand. Completion
	// scripts are still reachable via the GenZshCompletion / GenBashCompletion
	// / GenFishCompletion methods that `gg shell install` calls — we just
	// don't want a parallel user-facing surface to confuse "how do I install".
	root.CompletionOptions.HiddenDefaultCmd = true
}

// Group IDs / titles. IDs are stable string keys cobra uses to assign
// commands to groups; titles are what `gg --help` renders. Both
// lowercase by convention. Order here is the order groups print in.
const (
	groupCommits    = "commits"
	groupStacks     = "stacks"
	groupNavigation = "navigation"
	groupRemotes    = "remotes"
	groupConflicts  = "conflicts"
	groupAdmin      = "admin"
)

// commandGroups defines the gg command taxonomy in render order. New
// commands should be added to the most user-intent-matching group; if
// nothing fits, prefer adding a new group over a stray (cobra would
// otherwise bucket it under "additional").
type commandGroup struct {
	id, title string
	cmds      []*cobra.Command
}

func commandGroups() []commandGroup {
	return []commandGroup{
		{groupCommits, groupCommits, []*cobra.Command{
			newStatusCmd(), newLogCmd(), newDiffCmd(),
			newAddCmd(), newCommitCmd(), newAmendCmd(),
			newStashCmd(), newResetCmd(), newBlameCmd(),
			newRmCmd(), newMvCmd(),
		}},
		{groupStacks, groupStacks, []*cobra.Command{
			newNewCmd(), newAppendCmd(), newFoldCmd(),
			newRenameCmd(), newDeleteCmd(),
			newTrackCmd(), newUntrackCmd(), newRestackCmd(),
		}},
		{groupNavigation, groupNavigation, []*cobra.Command{
			newCdCmd(), newCheckoutCmd(),
			newUpstreamCmd(), newDownstreamCmd(),
			newFirstCmd(), newLastCmd(),
			newTrunkCmd(), newReposCmd(),
		}},
		{groupRemotes, groupRemotes, []*cobra.Command{
			newFetchCmd(), newSyncCmd(), newSubmitCmd(),
		}},
		{groupConflicts, groupConflicts, []*cobra.Command{
			newContinueCmd(), newAbortCmd(),
		}},
		{groupAdmin, groupAdmin, []*cobra.Command{
			newInitCmd(), newCloneCmd(), newLinkCmd(),
			newCleanupCmd(), newConfigCmd(), newShellCmd(), newVersionCmd(),
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
