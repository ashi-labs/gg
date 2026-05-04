package cli

import (
	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/sync"
	"github.com/spf13/cobra"
)

func newAmendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "amend [paths...]",
		Short: "Amend the tip commit and restack any descendant branches.",
		Long: `amend rewrites the current branch's tip commit, then — because the
tip SHA changes — restacks every descendant branch onto the new tip
so the rest of the stack stays coherent. This is the killer move for
stacked PRs: address review feedback on PR #2 of 4 without piling up
"fixup" commits, and downstream branches catch up automatically.

Flags:
  -a, --all       stage every tracked, modified file before amending.
  -m, --message   replace the commit message (skips the editor).
  --no-edit       keep the existing message (the common case for
                  "amend in my latest fix").
  --no-verify     bypass pre-commit and commit-msg hooks.

Behaviour by branch:
  - leaf (no children):  amends; no restack needed.
  - non-tip stack branch: amends, then restacks descendants.
  - trunk with stacks built on it: amends, then restacks every stack.

Refuses to run when a sync/restack is paused on a conflict — finish
that with ` + "`gg continue`" + ` or ` + "`gg abort`" + ` first, then amend.`,
		Args: cobra.ArbitraryArgs,
		RunE: runAmend,
	}
	cmd.Flags().BoolP("all", "a", false, "stage all tracked, modified files before amending")
	cmd.Flags().StringP("message", "m", "", "replace the commit message (skips the editor)")
	cmd.Flags().Bool("no-edit", false, "keep the existing commit message")
	cmd.Flags().Bool("no-verify", false, "bypass pre-commit and commit-msg hooks")
	return cmd
}

func runAmend(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	all, _ := cmd.Flags().GetBool("all")
	msg, _ := cmd.Flags().GetString("message")
	noEdit, _ := cmd.Flags().GetBool("no-edit")
	noVerify, _ := cmd.Flags().GetBool("no-verify")
	gitArgs := []string{"commit", "--amend"}
	if all {
		gitArgs = append(gitArgs, "-a")
	}
	switch {
	case msg != "":
		gitArgs = append(gitArgs, "-m", msg)
	case noEdit:
		gitArgs = append(gitArgs, "--no-edit")
	}
	if noVerify {
		gitArgs = append(gitArgs, "--no-verify")
	}
	if len(args) > 0 {
		gitArgs = append(gitArgs, "--")
		gitArgs = append(gitArgs, args...)
	}
	if err := gitx.In(cwd).Cmd(gitArgs...).Pipe().Run(); err != nil {
		return err
	}
	return restackAfterAmend()
}

// restackAfterAmend triggers a no-fetch restack scoped to whatever the
// rewritten tip can have invalidated. Unlike `commit`, amend MUST do
// this: amend replaces the tip SHA, orphaning any descendants whose
// ParentSHA pointed at the old tip. Without restack, the chain is
// broken until the next `gg sync`.
//
// Scope rules:
//   - On trunk with at least one child stack: whole-repo restack
//     (Scope{}). Every root's parent SHA just shifted.
//   - On a non-trunk branch with descendants: stack-scoped restack
//     (Scope.StackOf). Other stacks are untouched; the engine's
//     parent-SHA skip handles unrelated branches cheaply.
//   - On a leaf (no descendants): no-op. Nothing downstream to fix.
func restackAfterAmend() error {
	current, err := gitx.Revision.CurrentBranch(cwd)
	if err != nil {
		return err
	}
	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}
	l := stack.Build(repo.Trunk, branches)

	scope := sync.Scope{}
	switch current {
	case repo.Trunk:
		if len(l.Roots()) == 0 {
			return nil
		}
		// empty scope = whole-repo restack
	default:
		if len(l.Children(current)) == 0 {
			return nil
		}
		scope.StackOf = current
	}

	title := "restacking " + repo.ShortName()
	if scope.StackOf != "" {
		title += " · " + scope.StackOf
	}
	return runSyncWithProgress(sync.RunOpts{NoFetch: true, Scope: scope}, title, nil)
}
