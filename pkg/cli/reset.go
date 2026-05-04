package cli

import (
	"fmt"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/spf13/cobra"
)

func newResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset [--soft|--mixed|--hard] [<commit>] [paths...]",
		Short: "unstage paths or move head with --soft/--mixed/--hard.",
		Long: `wraps ` + "`git reset`" + `. with no flags, unstages everything (or the
named paths). with --soft, --mixed (default), or --hard plus a
commit-ish, moves the current branch's head.

mode quick reference:
  --soft <commit>   move head; leave the index and working tree alone.
  --mixed <commit>  move head; reset the index; leave the working tree.
  --hard <commit>   move head; reset the index AND the working tree.
                    destructive — uncommitted edits are lost.

soft and mixed leave the old tip reachable in the graph, so any
descendant branches stay valid (just out of sync with where the
parent ref now points). run ` + "`gg restack`" + ` when you want descendants
to follow.

--hard refuses to run without -y/--yes outside a tty; on a tty it
prompts. on success it triggers a stack-scoped restack so descendants
catch up to the rewritten head.`,
		Args:              cobra.ArbitraryArgs,
		RunE:              runReset,
		ValidArgsFunction: completeResetArgs,
	}
	cmd.Flags().Bool("soft", false, "move head; keep the index and working tree")
	cmd.Flags().Bool("mixed", false, "move head; reset the index; keep the working tree")
	cmd.Flags().Bool("hard", false, "move head; reset the index AND working tree (destructive)")
	cmd.Flags().BoolP("yes", "y", false, "skip the --hard confirmation prompt")
	return cmd
}

func runReset(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	soft, _ := cmd.Flags().GetBool("soft")
	mixed, _ := cmd.Flags().GetBool("mixed")
	hard, _ := cmd.Flags().GetBool("hard")
	if moreThanOne(soft, mixed, hard) {
		return fmt.Errorf("--soft, --mixed, --hard are mutually exclusive")
	}

	if hard {
		return runResetHard(cmd, args)
	}

	gitArgs := []string{"reset"}
	switch {
	case soft:
		gitArgs = append(gitArgs, "--soft")
	case mixed:
		gitArgs = append(gitArgs, "--mixed")
	}
	gitArgs = append(gitArgs, args...)
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}

// runResetHard handles the destructive case: confirm, run, then restack
// descendants so they don't dangle off the now-removed tip.
func runResetHard(cmd *cobra.Command, args []string) error {
	skipConfirm, _ := cmd.Flags().GetBool("yes")
	if !skipConfirm {
		target := "HEAD"
		if len(args) > 0 {
			target = args[0]
		}
		ok, err := confirmYesNo(fmt.Sprintf(
			"hard-reset to %s? this discards uncommitted changes and moves the branch ref.",
			target,
		))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("aborted")
		}
	}
	gitArgs := append([]string{"reset", "--hard"}, args...)
	if err := gitx.In(cwd).Cmd(gitArgs...).Pipe().Run(); err != nil {
		return err
	}
	// Same scoping rules as amend: only restack when there's something
	// downstream that could be invalidated.
	return restackAfterAmend()
}

func moreThanOne(bs ...bool) bool {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n > 1
}

// completeResetArgs offers commit-ish targets at position 0 and
// stageable paths after that. Commit-ishes include the canonical
// `HEAD~N` ladder plus the most recent ten commits (short sha +
// subject as the cobra description).
//
// When --soft/--mixed/--hard is set, every positional is a commit-ish
// — git reset doesn't accept paths in mode-flag form — so only
// commit-ishes are offered regardless of position.
func completeResetArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if repo == nil {
		return nil, cobra.ShellCompDirectiveDefault
	}
	soft, _ := cmd.Flags().GetBool("soft")
	mixed, _ := cmd.Flags().GetBool("mixed")
	hard, _ := cmd.Flags().GetBool("hard")
	modeFlagSet := soft || mixed || hard

	if len(args) == 0 || modeFlagSet {
		return commitishCompletions(), cobra.ShellCompDirectiveNoFileComp
	}
	// First positional already given (must be a commit-ish in
	// path-restore mode); subsequent positionals are paths.
	return completeStageablePaths(cmd, args, toComplete)
}

// commitishCompletions returns HEAD, HEAD~1, then the ten most recent
// short SHAs annotated with their subjects. The HEAD~ ladder stops at
// 1 because anything further back is more clearly named by SHA in the
// list below it.
func commitishCompletions() []string {
	out := []string{
		"HEAD\tcurrent commit",
		"HEAD~1\tone commit back",
	}
	lines, err := gitx.In(cwd).Cmd("log", "-n", "10", "--format=%h\t%s").Lines()
	if err != nil {
		return out
	}
	return append(out, lines...)
}
