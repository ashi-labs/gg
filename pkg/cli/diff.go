package cli

import (
	"fmt"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "diff [branch] [paths...]",
		Aliases: []string{"d"},
		Short:   "show changes in the current worktree, against the index, parent, or another branch.",
		Long: `shows pending changes. the default comparison is working tree vs head
(equivalent to ` + "`git diff`" + `). --staged compares the index to head;
--parent compares head to this branch's stack parent. --parent errors
on trunk because trunk has no parent.

a positional <branch> compares head to that branch (<branch>..HEAD).
trailing positional args are paths that scope the diff. use ` + "`--`" + ` to
force path interpretation when a path shares a name with a branch
(e.g. ` + "`gg diff -- feat-a`" + `).

output streams through git's pager and color settings unchanged.`,
		Args:              cobra.ArbitraryArgs,
		RunE:              runDiff,
		ValidArgsFunction: completeDiffArgs,
	}
	cmd.Flags().Bool("staged", false, "diff index vs HEAD instead of working tree vs HEAD")
	cmd.Flags().Bool("parent", false, "diff HEAD vs this branch's stack parent")
	return cmd
}

func runDiff(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	staged, _ := cmd.Flags().GetBool("staged")
	parent, _ := cmd.Flags().GetBool("parent")

	if staged && parent {
		return fmt.Errorf("--staged and --parent are mutually exclusive")
	}

	// Split positional args into an optional leading <branch> and the
	// remaining path scope. A leading arg counts as a branch only if it
	// matches trunk or a tracked branch — keeps `gg diff foo.go` (where
	// foo.go isn't a branch) routing to a path-only diff.
	//
	// `--` forces every arg into path mode. Cobra strips `--` from args
	// before RunE sees them, but exposes the original position via
	// ArgsLenAtDash: -1 means "no --", 0 means "-- came first" (all args
	// are paths).
	forcePath := cmd.Flags().ArgsLenAtDash() == 0
	branchArg, paths := splitDiffPositionals(args, forcePath)
	if branchArg != "" && (staged || parent) {
		return fmt.Errorf("can't combine a branch argument with --staged or --parent")
	}

	gitArgs := []string{"diff"}
	switch {
	case staged:
		gitArgs = append(gitArgs, "--staged")
	case parent:
		current, err := gitx.Revision.CurrentBranch(cwd)
		if err != nil {
			return err
		}
		if current == repo.Trunk || current == "" || current == "HEAD" {
			return fmt.Errorf("--parent has no meaning on %s (trunk has no parent)", current)
		}
		b, err := state.LoadBranch(bare, current)
		if err != nil || b.Parent == "" {
			return fmt.Errorf("%s isn't tracked by gg — no parent recorded", current)
		}
		gitArgs = append(gitArgs, b.Parent+"..HEAD")
	case branchArg != "":
		gitArgs = append(gitArgs, branchArg+"..HEAD")
	}

	if len(paths) > 0 {
		gitArgs = append(gitArgs, "--")
		gitArgs = append(gitArgs, paths...)
	}
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}

// splitDiffPositionals decides whether the first positional arg is the
// optional <branch> selector or just the first path. A leading arg is
// treated as a branch only when it matches trunk or a tracked branch
// name; everything else is a path. forcePath=true (set when the user
// typed `--` before any arg) forces every arg into path mode regardless.
func splitDiffPositionals(args []string, forcePath bool) (branch string, paths []string) {
	if len(args) == 0 {
		return "", nil
	}
	if forcePath || !isKnownBranch(args[0]) {
		return "", args
	}
	return args[0], args[1:]
}

func isKnownBranch(name string) bool {
	if name == "" || repo == nil {
		return false
	}
	if name == repo.Trunk {
		return true
	}
	branches, err := state.AllBranches(bare)
	if err != nil {
		return false
	}
	for _, b := range branches {
		if b.Name == name {
			return true
		}
	}
	return false
}

// completeDiffArgs offers branch names at position 0 and stageable paths
// for every position after that. Path candidates are the same set `gg
// add` offers — what's currently dirty in the worktree — since that's
// what users most often want to scope a diff to.
func completeDiffArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if repo == nil {
		return nil, cobra.ShellCompDirectiveDefault
	}
	if len(args) == 0 {
		// Mix branches and paths at position 0 so either workflow has
		// completion. Branches go first so they sort/show prominently.
		out := []string{repo.Trunk + "\ttrunk"}
		if branches, err := state.AllBranches(bare); err == nil {
			for _, b := range branches {
				out = append(out, b.Name+"\toff "+b.Parent)
			}
		}
		paths, _ := completeStageablePaths(cmd, args, toComplete)
		out = append(out, paths...)
		return out, cobra.ShellCompDirectiveNoFileComp
	}
	return completeStageablePaths(cmd, args, toComplete)
}
