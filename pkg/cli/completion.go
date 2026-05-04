package cli

import (
	"fmt"
	"strings"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/registry"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/spf13/cobra"
)

// Cobra takes completion entries of the form "<word>\t<description>" and
// (when the shell supports it — zsh always, bash with extensions) renders
// the description as a second column. Keep descriptions short: zsh
// truncates aggressively on narrow terminals.

// compOpts tunes the branch-completion helpers.
type compOpts struct {
	IncludeTrunk   bool
	ExcludeCurrent bool
	ExcludeTrunk   bool
}

// completeBranches returns a cobra completion function for commands whose
// first positional argument is a branch. Runs on every <Tab> press, so it
// relies on batched AllBranches for speed (one git invocation, <15 ms).
func completeBranches(
	opts compOpts,
) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		branches, err := state.AllBranches(bare)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		current, _ := gitx.Revision.CurrentBranch(cwd)
		l := stack.Build(repo.Trunk, branches)

		var out []string
		if opts.IncludeTrunk && !opts.ExcludeTrunk {
			trunkDesc := "trunk"
			if current == repo.Trunk {
				trunkDesc = "trunk (current)"
			}
			out = append(out, repo.Trunk+"\t"+trunkDesc)
		}
		for _, b := range branches {
			if opts.ExcludeCurrent && b.Name == current {
				continue
			}
			out = append(out, b.Name+"\t"+branchDescription(l, b, current))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// branchDescription produces the short right-column text shown beside a
// branch in completion output. Intentionally terse — shells truncate.
func branchDescription(l stack.Lineage, b state.Branch, current string) string {
	parts := []string{"off " + b.Parent}
	if kids := l.Children(b.Name); len(kids) > 0 {
		parts = append(parts, fmt.Sprintf("%d downstream", len(kids)))
	}
	if b.PRNumber > 0 {
		parts = append(parts, fmt.Sprintf("#%d", b.PRNumber))
	}
	if b.Name == current {
		parts = append(parts, "current")
	}
	return strings.Join(parts, ", ")
}

// completeRepos completes registered repo names, with trunk + origin in the
// description column.
func completeRepos(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	entries, err := registry.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		desc := e.Trunk
		if e.Origin != "" {
			desc += " · " + e.Origin
		}
		if e.Validate() != registry.StatusOK {
			desc += " (" + e.Validate().String() + ")"
		}
		out = append(out, e.Name+"\t"+desc)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
