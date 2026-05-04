package cli

import (
	"fmt"
	"strings"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [paths...]",
		Short: "Stage paths in the current worktree for the next commit.",
		Long: `add stages paths for the next commit. With -a, every change in the
current worktree is staged — modified, deleted, AND untracked.
-a is mutually exclusive with explicit paths.

Tab completion suggests addable files (modified, deleted, or untracked) — 
fully-clean paths aren't offered.`,
		Args:              cobra.ArbitraryArgs,
		RunE:              runAdd,
		ValidArgsFunction: completeStageablePaths,
	}
	cmd.Flags().
		BoolP("all", "a", false, "stage everything in the current worktree, including untracked (mirrors `git add -A`)")
	return cmd
}

func runAdd(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	all, _ := cmd.Flags().GetBool("all")
	if all && len(args) > 0 {
		return fmt.Errorf("-a stages everything; don't pass paths with it")
	}
	if !all && len(args) == 0 {
		return fmt.Errorf("nothing to add — pass paths or use -a to stage everything")
	}
	gitArgs := []string{"add"}
	if all {
		gitArgs = append(gitArgs, "-A")
	} else {
		gitArgs = append(gitArgs, "--")
		gitArgs = append(gitArgs, args...)
	}
	return gitx.In(cwd).Cmd(gitArgs...).Run()
}

// completeStageablePaths offers tab-completion for any path with pending
// changes in the current worktree (modified, deleted, untracked, or with
// staged-but-still-dirty unstaged changes). Driven off `git status
// --porcelain`, with path shape (./ prefix, subdir-relative, ...) handled
// by pathSuggester.
//
// Falls back to default file completion on error or outside a tracked repo —
// better to over-offer than to silently produce no suggestions.
func completeStageablePaths(
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
	// --untracked-files=all expands untracked dirs into per-file entries.
	// Without it, porcelain reports an untracked dir as a single "?? pkg/"
	// line, which hides nested files from completion.
	raw, err := gitx.In(cwd).Cmd("status", "--porcelain", "--untracked-files=all").Bytes()
	if err != nil {
		return nil, cobra.ShellCompDirectiveDefault
	}
	seen := make(map[string]bool, len(args))
	for _, a := range args {
		seen[a] = true
	}
	var out []string
	// Split on raw bytes — Lines() routes through String()/TrimSpace,
	// which eats the leading space on entries like " M path"
	// (unstaged-modified) when they're the first line of output, chopping
	// one char off the path.
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		// Porcelain v1 format: "XY␠path" (XY is two status chars). Rename
		// entries embed " -> " between old and new path; for staging
		// purposes the new path is what we want.
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		s := sug.suggest(path)
		if s == "" || seen[s] {
			continue
		}
		out = append(out, s)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
