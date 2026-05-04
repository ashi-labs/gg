package cli

import (
	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/spf13/cobra"
)

func newBlameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blame <file>",
		Short: "show what revision and author last modified each line of a file.",
		Long: `wraps ` + "`git blame`" + `. all positional args and flags pass through
unchanged, including ` + "`-L <start>,<end>`" + ` for line ranges and
` + "`-w`" + ` to ignore whitespace changes. output streams through git's
pager and color settings.`,
		Args:              cobra.MinimumNArgs(1),
		RunE:              runBlame,
		ValidArgsFunction: completeTrackedPaths,
	}
	cmd.DisableFlagParsing = true
	return cmd
}

// completeTrackedPaths offers tracked files as completion candidates,
// shaped through pathSuggester so `./`, `../`, and subdir-relative
// inputs all resolve correctly. Used by blame (and any future read-
// only inspection command) where the typical target is a clean,
// tracked file rather than something with pending edits.
func completeTrackedPaths(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if repo == nil {
		return nil, cobra.ShellCompDirectiveDefault
	}
	sug, err := newPathSuggester(cwd, toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveDefault
	}
	paths, err := trackedPathLines(cwd)
	if err != nil {
		return nil, cobra.ShellCompDirectiveDefault
	}
	seen := make(map[string]bool, len(args))
	for _, a := range args {
		seen[a] = true
	}
	var out []string
	for _, p := range paths {
		s := sug.suggest(p)
		if s == "" || seen[s] {
			continue
		}
		out = append(out, s)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func runBlame(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	gitArgs := append([]string{"blame", "--color-by-age"}, args...)
	return gitx.In(cwd).Cmd(gitArgs...).Pipe().Run()
}
