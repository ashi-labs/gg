package cli

import (
	"fmt"
	"sort"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/tui/picker"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete [branch]",
		Aliases: []string{"rm"},
		Short:   "Remove a branch and its worktree; reparent children onto the parent.",
		Long: `With no argument, opens a picker listing tracked branches (excluding the
current one, which can't be deleted while you're standing in it).

With --recursive (-r), deletes the target and every branch downstream of it.
Refuses if your current branch is in the doomed set — ` + "`gg upstream`" + ` out first.`,
		Args:              cobra.RangeArgs(0, 1),
		RunE:              runDelete,
		ValidArgsFunction: completeBranches(compOpts{ExcludeCurrent: true}),
	}
	cmd.Flags().
		BoolP("recursive", "r", false, "recursively delete every branch downstream of the target")
	cmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func runDelete(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	var name string
	if len(args) == 1 {
		name = args[0]
	} else {
		n, err := pickDeletionTarget()
		if err != nil {
			return err
		}
		if n == "" {
			return nil
		}
		name = n
	}
	if name == repo.Trunk {
		return fmt.Errorf("cannot delete trunk (%s)", repo.Trunk)
	}
	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}
	lineage := stack.Build(repo.Trunk, branches)
	if !lineage.Contains(name) {
		return fmt.Errorf("%s is not tracked", name)
	}
	recursive, _ := cmd.Flags().GetBool("recursive")
	yes, _ := cmd.Flags().GetBool("yes")
	current, _ := gitx.Revision.CurrentBranch(cwd)
	var derr error
	if recursive {
		derr = deleteSubtree(lineage, name, current, yes)
	} else {
		derr = deleteOne(lineage, name, current, yes)
	}
	if derr != nil {
		return derr
	}
	// Refresh the stack-footer on remaining open PRs so the tree no
	// longer shows the deleted branch (and, with --recursive, every
	// downstream we just removed). Best-effort — helper no-ops without
	// gh / origin and logs per-PR errors rather than failing the
	// delete, which has already committed locally.
	_ = refreshOpenPRFooters()
	return nil
}

// deleteOne removes a single branch, reparenting its children onto its parent.
func deleteOne(lineage stack.Lineage, name, current string, skipConfirm bool) error {
	if current == name {
		return fmt.Errorf(
			"cannot delete %s while you're in its worktree — `gg upstream` first, then re-run",
			name,
		)
	}
	b := lineage.ByName[name]
	kids := lineage.Children(name)
	for _, kid := range kids {
		if err := state.UpdateParent(bare, kid, b.Parent); err != nil {
			return err
		}
		if sha, err := gitx.Revision.HeadSHA(bare, b.Parent); err == nil {
			_ = state.UpdateParentSHA(bare, kid, sha)
		}
	}
	if err := removeBranch(b, skipConfirm, true); err != nil {
		return err
	}
	if len(kids) > 0 {
		successf(
			"Reparented %d %s from %s onto %s",
			len(kids),
			plural(len(kids), "child", "children"),
			styleBranch(name),
			styleBranch(b.Parent),
		)
	}
	return nil
}

// deleteSubtree removes the target plus every branch downstream of it.
// Leaves are removed first so `git worktree remove` doesn't trip over
// still-attached descendants.
func deleteSubtree(lineage stack.Lineage, name, current string, skipConfirm bool) error {
	doomed := append([]string{name}, lineage.Descendants(name)...)
	doomedSet := make(map[string]bool, len(doomed))
	for _, n := range doomed {
		doomedSet[n] = true
	}
	if doomedSet[current] {
		return fmt.Errorf(
			"cannot delete this subtree while you're inside it (%s) — `gg upstream` out first, then re-run",
			current,
		)
	}
	// Precheck all worktrees for dirty state up front so we don't half-delete.
	for _, n := range doomed {
		b := lineage.ByName[n]
		if b.Worktree == "" {
			continue
		}
		dirty, err := gitx.Status.IsDirty(b.Worktree)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf(
				"%s has uncommitted changes in %s; commit or stash first",
				n,
				b.Worktree,
			)
		}
	}
	// Delete leaf-first: iterate the topological order in reverse, filtering
	// to the doomed set. That guarantees a branch's children are gone before
	// we try to remove its worktree.
	topo := lineage.Topological()
	for i := len(topo) - 1; i >= 0; i-- {
		n := topo[i]
		if !doomedSet[n] {
			continue
		}
		if err := removeBranch(lineage.ByName[n], skipConfirm, true); err != nil {
			return err
		}
	}
	return nil
}

func removeBranch(b state.Branch, skipConfirm bool, renderOutput bool) error {
	if !skipConfirm {
		prompt := shintf("prune branch: %s?", styleBranch(b.Name))
		if hasUnpushedCommits(b) {
			prompt = shintf(
				"prune branch: %s? found unpushed commits! they will be lost!",
				styleBranch(b.Name),
			)
		}
		ok, err := confirmYesNo(prompt)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("cancelled")
		}
	}
	if b.Worktree != "" {
		if err := gitx.Worktree.Remove(bare, b.Worktree); err != nil {
			return fmt.Errorf("removing worktree %s: %w", b.Worktree, err)
		}
	}
	if err := gitx.Branch.Delete(bare, b.Name); err != nil {
		return fmt.Errorf("deleting branch %s: %w", b.Name, err)
	}
	if err := state.DeleteBranch(bare, b.Name); err != nil {
		return err
	}
	if renderOutput {
		successf("pruned branch: %s", styleBranch(b.Name))
	}
	return nil
}

// hasUnpushedCommits reports whether b's tip carries commits that
// don't exist on origin/<b.Name>. Used by removeBranch to upgrade
// the prune confirm into an explicit "you're about to lose work"
// warning.
//
// The check is intentionally simple: compare local tip to origin tip
// directly rather than walking the commit graph. False positives
// (branch tip matches its parent — no unique commits — but the
// branch was never pushed) are filtered out by the parent-tip check
// up front, which catches the common "fresh empty branch I never
// touched" case. Without an origin remote configured, we err toward
// warning, since there's literally no remote to recover from.
func hasUnpushedCommits(b state.Branch) bool {
	work := b.Worktree
	if work == "" {
		work = bare
	}
	local, _ := gitx.Revision.HeadSHA(work, b.Name)
	if local == "" {
		return false
	}
	if b.Parent != "" {
		if parent, _ := gitx.Revision.HeadSHA(work, b.Parent); parent == local {
			return false
		}
	}
	if !gitx.Remote.Exists(bare, "origin") {
		return true
	}
	remote, _ := gitx.Revision.HeadSHA(work, "origin/"+b.Name)
	return remote == "" || remote != local
}

// pickDeletionTarget shows a picker of tracked branches (excluding current
// and trunk). Returns "" with nil error on cancel.
func pickDeletionTarget() (string, error) {
	branches, err := state.AllBranches(bare)
	if err != nil {
		return "", err
	}
	current, _ := gitx.Revision.CurrentBranch(cwd)

	items := make([]picker.Item, 0, len(branches))
	for _, b := range branches {
		if b.Name == current || b.Name == repo.Trunk {
			continue
		}
		// Keep the coloring consistent with `gg checkout` and `gg cd`:
		// Branch (blue) in the normal row, Dirty (orange) with the name
		// underlined when the cursor lands on it. The parent tag tracks
		// the row's state so hover doesn't leave half the line mid-dim.
		marker := style.Glyphs.Branch + " "
		label := style.Stderr.Branch.Render(marker+b.Name) +
			"  " + style.Stderr.Dim.Render("(parent: ") +
			styleBranch(b.Parent) +
			style.Stderr.Dim.Render(")")
		hover := style.Stderr.Dirty.Render(marker) +
			style.Stderr.Dirty.Underline(true).Render(b.Name) +
			"  " + style.Stderr.Dirty.Render("(parent: "+b.Parent+")")
		items = append(items, picker.Item{
			Branch:     b.Name,
			Label:      label,
			HoverLabel: hover,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Branch < items[j].Branch })

	if len(items) == 0 {
		if current != "" && current != repo.Trunk {
			return "", fmt.Errorf(
				"no tracked branches available to delete (the only candidate is your current branch — `gg upstream` first, or pass a branch by name)",
			)
		}
		return "", fmt.Errorf("no tracked branches to delete")
	}
	chosen, ok, err := picker.Select(items, "Select a branch to delete")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	return chosen.Branch, nil
}
