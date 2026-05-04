package cli

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/tui/picker"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/spf13/cobra"
)

type navDir int

const (
	navUpstream   navDir = iota // toward trunk (one step closer to the parent)
	navDownstream               // toward leaf (one step closer to the child)
	navFirst                    // jump to the first entry of the stack (direct child of trunk)
	navLast                     // jump to the last entry of the stack (the leaf)
	navTrunk                    // jump to trunk
)

func newUpstreamCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "upstream [n]",
		Aliases: []string{"up"},
		Short:   "Move n steps toward trunk (toward the parent). Default 1.",
		Args:    cobra.RangeArgs(0, 1),
		RunE:    func(cmd *cobra.Command, args []string) error { return runNav(args, navUpstream) },
	}
}

func newDownstreamCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "downstream [n]",
		Aliases: []string{"down"},
		Short:   "Move n steps toward the leaf (toward the child). Default 1.",
		Args:    cobra.RangeArgs(0, 1),
		RunE:    func(cmd *cobra.Command, args []string) error { return runNav(args, navDownstream) },
	}
}

func newFirstCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "first",
		Aliases: []string{"1"},
		Short:   "Jump to the first entry of the current stack (direct child of trunk).",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return runNav(nil, navFirst) },
	}
}

func newLastCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "last",
		Aliases: []string{"N"},
		Short:   "Jump to the last entry of the current stack (the leaf).",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return runNav(nil, navLast) },
	}
}

func newTrunkCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "trunk",
		Aliases: []string{"0"},
		Short:   "Jump to trunk.",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return runNav(nil, navTrunk) },
	}
}

func runNav(args []string, dir navDir) error {
	if repo == nil {
		return ctxResolutionErr
	}
	steps := 1
	if len(args) == 1 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			return fmt.Errorf("n must be a positive integer")
		}
		steps = n
	}
	current, err := gitx.Revision.CurrentBranch(cwd)
	if err != nil {
		return err
	}
	if current == "HEAD" || current == "" {
		return fmt.Errorf("detached HEAD; checkout a branch first")
	}
	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}
	lineage := stack.Build(repo.Trunk, branches)

	if current == repo.Trunk {
		switch dir {
		case navLast, navFirst:
			return fmt.Errorf(
				"you're on trunk (%s) — `gg cd` first to pick a branch",
				repo.Trunk,
			)
		case navUpstream, navTrunk:
			return fmt.Errorf("you're already on trunk (%s)", repo.Trunk)
		}
	}

	target := current
	switch dir {
	case navUpstream:
		for i := 0; i < steps; i++ {
			parent := lineage.Parent(target)
			if parent == "" {
				parent = repo.Trunk
			}
			target = parent
			if target == repo.Trunk {
				break
			}
		}
	case navDownstream:
		for i := 0; i < steps; i++ {
			kids := lineage.Children(target)
			if len(kids) == 0 {
				if target == current {
					if current == repo.Trunk {
						return fmt.Errorf(
							"no tracked branches yet — `gg new <name>` to start a stack",
						)
					}
					return fmt.Errorf(
						"%s is already the last entry in the stack (no downstream branch)",
						current,
					)
				}
				break
			}
			next, ok, err := pickDownstream(target, kids)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			target = next
		}
	case navLast:
		for {
			kids := lineage.Children(target)
			if len(kids) == 0 {
				break
			}
			sort.Strings(kids)
			target = kids[0]
		}
		if target == current {
			return fmt.Errorf("%s is already the last entry in the stack", current)
		}
	case navFirst:
		for {
			parent := lineage.Parent(target)
			if parent == "" || parent == repo.Trunk {
				break
			}
			target = parent
		}
		if target == current {
			return fmt.Errorf("%s is already the first entry in the stack", current)
		}
	case navTrunk:
		target = repo.Trunk
	}

	if target == repo.Trunk {
		stdout(repo.PrimaryWorktree)
		return nil
	}
	pb, err := state.LoadBranch(bare, target)
	if err != nil {
		return err
	}
	if pb.Worktree == "" {
		return fmt.Errorf("no worktree recorded for %s", target)
	}
	stdout(pb.Worktree)
	return nil
}

// pickDownstream chooses which child to descend into. One child → return
// it directly (no prompt). Multiple children → open an interactive
// picker so the user doesn't get silently railroaded onto the
// alphabetical first kid. ok=false means the user canceled the picker;
// caller should abort navigation without writing a path.
func pickDownstream(parent string, kids []string) (string, bool, error) {
	sort.Strings(kids)
	if len(kids) == 1 {
		return kids[0], true, nil
	}
	items := make([]picker.Item, 0, len(kids))
	for _, name := range kids {
		marker := style.Glyphs.Branch + " "
		label := style.Stderr.Branch.Render(marker + name)
		hover := style.Stderr.Dirty.Render(marker) +
			style.Stderr.Dirty.Underline(true).Render(name)
		items = append(items, picker.Item{
			Branch:     name,
			Label:      label,
			HoverLabel: hover,
		})
	}
	chosen, ok, err := picker.Select(
		items,
		fmt.Sprintf("%s has %d children — pick one to descend into", parent, len(kids)),
	)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	return chosen.Branch, true, nil
}
