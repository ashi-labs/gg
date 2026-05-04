package cli

import (
	"fmt"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/spf13/cobra"
)

func newFoldCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fold",
		Short: "squash the current branch's commits into its parent; reparent children onto the parent.",
		Args:  cobra.NoArgs,
		RunE:  runFold,
	}
	cmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func runFold(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	current, err := gitx.Revision.CurrentBranch(cwd)
	if err != nil {
		return err
	}
	if current == "HEAD" || current == "" {
		return fmt.Errorf("detached HEAD; checkout a branch first")
	}
	if current == repo.Trunk {
		return fmt.Errorf("cannot fold trunk")
	}
	b, err := state.LoadBranch(bare, current)
	if err != nil {
		return err
	}
	if b.Parent == "" {
		return fmt.Errorf("%s is not tracked", current)
	}
	parent := b.Parent
	parentPath := repo.PrimaryWorktree
	if parent != repo.Trunk {
		pb, err := state.LoadBranch(bare, parent)
		if err != nil {
			return err
		}
		if pb.Worktree == "" {
			return fmt.Errorf("no worktree recorded for parent %s", parent)
		}
		parentPath = pb.Worktree
	}
	// Both worktrees must be clean: we're committing into parent and deleting current.
	if dirty, err := gitx.Status.IsDirty(b.Worktree); err != nil {
		return err
	} else if dirty {
		return fmt.Errorf(
			"%s has uncommitted changes in %s; commit or stash first",
			current,
			b.Worktree,
		)
	}
	if dirty, err := gitx.Status.IsDirty(parentPath); err != nil {
		return err
	} else if dirty {
		return fmt.Errorf(
			"parent %s has uncommitted changes in %s; commit or stash first",
			parent,
			parentPath,
		)
	}

	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}
	l := stack.Build(repo.Trunk, branches)
	kids := l.Children(current)

	// Confirm before the squash + delete. Fold is destructive in two ways:
	// it rewrites parent's history (a new squash commit) and it deletes the
	// current branch + its worktree. The prompt summarizes both effects.
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		title := fmt.Sprintf("fold %s into %s? (will delete %s)", current, parent, current)
		if len(kids) > 0 {
			title = fmt.Sprintf(
				"fold %s into %s? (will delete %s and reparent %d %s onto %s)",
				current, parent, current, len(kids), plural(len(kids), "child", "children"), parent,
			)
		}
		ok, err := confirmYesNo(title)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("cancelled")
		}
	}

	// Record current's tip before we delete it — children's commits branched
	// from here and we'll need this SHA as the `--upstream` for their rebase.
	oldCurrentSHA, err := gitx.Revision.HeadSHA(bare, current)
	if err != nil {
		return err
	}

	// 1. Squash-merge current into parent. --ff is fine; the squash commit is
	//    always a new commit on parent's tip.
	if err := gitx.Merge.Squash(parentPath, current); err != nil {
		return fmt.Errorf("merge --squash failed: %w", err)
	}
	if err := gitx.Commit.Create(parentPath, "fold "+current); err != nil {
		return fmt.Errorf("commit after squash failed: %w", err)
	}

	newParentSHA, err := gitx.Revision.HeadSHA(bare, parent)
	if err != nil {
		return err
	}

	// 2. Rebase direct children onto parent's new tip. Grandchildren are left
	//    stale (parent-sha out of date); next `gg sync` handles them.
	for _, kid := range kids {
		kidB, err := state.LoadBranch(bare, kid)
		if err != nil {
			return fmt.Errorf("loading %s: %w", kid, err)
		}
		if kidB.Worktree == "" {
			return fmt.Errorf("no worktree for %s; cannot rebase it", kid)
		}
		if err := gitx.Rebase.Onto(kidB.Worktree, newParentSHA, oldCurrentSHA); err != nil {
			return fmt.Errorf(
				"rebasing %s onto %s failed: %w (resolve in %s, then re-run `gg sync`)",
				kid,
				parent,
				err,
				kidB.Worktree,
			)
		}
		if err := state.UpdateParent(bare, kid, parent); err != nil {
			return err
		}
		if err := state.UpdateParentSHA(bare, kid, newParentSHA); err != nil {
			return err
		}
	}

	// 3. Remove current's worktree and delete the branch.
	if err := gitx.Worktree.Remove(bare, b.Worktree); err != nil {
		return fmt.Errorf("removing worktree %s: %w", b.Worktree, err)
	}
	if err := gitx.Branch.Delete(bare, current); err != nil {
		return fmt.Errorf("deleting branch %s: %w", current, err)
	}
	if err := state.DeleteBranch(bare, current); err != nil {
		return err
	}

	if len(kids) > 0 {
		successf(
			"folded %s into %s  (reparented %d child(ren))",
			styleBranch(current),
			styleBranch(parent),
			len(kids),
		)
	} else {
		successf("folded %s into %s", styleBranch(current), styleBranch(parent))
	}
	// Refresh the stack-footer on remaining open PRs so the tree no
	// longer shows the folded branch. Best-effort — helper no-ops
	// without gh / origin and logs per-PR errors rather than failing
	// the fold, which has already committed locally.
	_ = refreshOpenPRFooters()
	stdout(parentPath)
	return nil
}
