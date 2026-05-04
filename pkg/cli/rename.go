package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/gitx/forge"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/spf13/cobra"
)

func newRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rename <new>",
		Aliases: []string{"mv"},
		Short:   "rename the current branch and its worktree directory.",
		Args:    cobra.ExactArgs(1),
		RunE:    runRename,
	}
}

func runRename(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	newName := args[0]
	current, err := gitx.Revision.CurrentBranch(cwd)
	if err != nil {
		return err
	}
	if current == "HEAD" || current == "" {
		return fmt.Errorf("detached HEAD; checkout a branch first")
	}
	if current == repo.Trunk {
		return fmt.Errorf("cannot rename trunk (%s)", repo.Trunk)
	}
	if newName == repo.Trunk {
		return fmt.Errorf("cannot rename to trunk name (%s)", repo.Trunk)
	}
	if newName == current {
		return fmt.Errorf("old and new name are the same")
	}

	b, err := state.LoadBranch(bare, current)
	if err != nil {
		return err
	}
	if b.Parent == "" {
		return fmt.Errorf("%s is not tracked (run `gg track` first)", current)
	}
	oldPath := b.Worktree
	if oldPath == "" {
		return fmt.Errorf("no worktree recorded for %s", current)
	}
	newPath := repo.WorktreePath(newName)
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("%s already exists", newPath)
	}
	if gitx.Branch.HasLocal(bare, newName) {
		return fmt.Errorf("branch %s already exists", newName)
	}
	if dirty, err := gitx.Status.IsDirty(oldPath); err != nil {
		return err
	} else if dirty {
		return fmt.Errorf("%s has uncommitted changes; commit or stash first", oldPath)
	}

	// Capture children and the old PR number before any config keys move.
	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}
	l := stack.Build(repo.Trunk, branches)
	kids := l.Children(current)
	oldPR := b.PRNumber

	// 1. Rename the git branch inside the old worktree. This updates
	//    refs/heads and the worktree's HEAD atomically.
	if err := gitx.Branch.Rename(oldPath, current, newName); err != nil {
		return err
	}

	// 2. Move the worktree via git itself. `git worktree move` relocates the
	//    directory AND fixes every metadata pointer (admin dir's gitdir, the
	//    worktree's .git pointer, its reflog paths). Replaces the previous
	//    hand-rolled os.Rename + patch-gitdir dance, which left stale
	//    references when the admin dir name didn't match the new path.
	if err := gitx.Worktree.Move(bare, oldPath, newPath); err != nil {
		return fmt.Errorf("moving worktree to %s: %w", newPath, err)
	}

	// 3. Rename the worktree admin dir ($bare/worktrees/<basename>) to match
	//    the new path's basename. `git worktree move` updates the admin
	//    dir's contents but NOT its own name — it treats the admin dir as
	//    a stable opaque ID. z4h's gitstatusd (the async engine behind most
	//    zsh prompt themes) caches per-worktree state keyed by this admin
	//    path, so leaving the old basename means gitstatusd serves stale
	//    "branch = <old name>" answers until the shell is exec'd fresh.
	//    Renaming it forces a cache-miss on the next prompt tick.
	if err := renameWorktreeAdminDir(newPath); err != nil {
		return err
	}

	// 4. Move gg config keys to the new branch name + update worktree path.
	if err := state.RenameBranch(bare, current, newName); err != nil {
		return err
	}
	if err := state.UpdateWorktree(bare, newName, newPath); err != nil {
		return err
	}

	// 5. Reparent children of the old name onto the new name.
	for _, kid := range kids {
		if err := state.UpdateParent(bare, kid, newName); err != nil {
			return err
		}
	}

	// 6. Mirror the rename on origin. Renaming a branch's head ref on GitHub
	//    implicitly closes any open PR from that branch — the API doesn't
	//    support changing a PR's head. So: delete old remote, push new, and
	//    (if there was a PR) open a replacement PR with the same base + body
	//    so the thread isn't lost to the ether.
	if gitx.Remote.Exists(bare, "origin") {
		if gitx.Remote.HasBranch(bare, current) {
			if err := recreateRemoteForRename(current, newName, newPath, oldPR); err != nil {
				return err
			}
		} else {
			// Old branch was local-only; still set upstream for the new name so
			// future pushes/pulls work without ceremony.
			if err := gitx.Remote.SetUpstreamConfig(newPath, newName); err != nil {
				return err
			}
		}
	}
	// Refresh the stack-footer on every other open PR so any tree their
	// body renders includes the new branch name. Best-effort: helper
	// no-ops without gh / origin and logs per-PR failures rather than
	// aborting the rename — the local rename has already succeeded and
	// the next `gg sync` will catch any stragglers.
	_ = refreshOpenPRFooters()
	stdout(newPath)
	return nil
}

// renameWorktreeAdminDir moves $bare/worktrees/<oldBase> to match the
// worktree's new path basename, then rewrites both halves of the
// gitdir/.git pointer pair so git still resolves correctly from either
// side. Safe no-op when the basename already matches.
func renameWorktreeAdminDir(newPath string) error {
	gitFilePath := filepath.Join(newPath, ".git")
	raw, err := os.ReadFile(gitFilePath)
	if err != nil {
		return err
	}
	oldAdmin := strings.TrimPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
	wantBase := filepath.Base(newPath)
	if filepath.Base(oldAdmin) == wantBase {
		return nil
	}
	newAdmin := filepath.Join(filepath.Dir(oldAdmin), wantBase)
	if _, err := os.Stat(newAdmin); err == nil {
		return fmt.Errorf("worktree admin dir %s already exists", newAdmin)
	}
	if err := os.Rename(oldAdmin, newAdmin); err != nil {
		return fmt.Errorf("renaming worktree admin dir: %w", err)
	}
	if err := os.WriteFile(gitFilePath, []byte("gitdir: "+newAdmin+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(newAdmin, "gitdir"),
		[]byte(gitFilePath+"\n"),
		0o644,
	); err != nil {
		return err
	}
	return nil
}

// recreateRemoteForRename deletes origin/<old> (which auto-closes any open
// PR from that branch), pushes the renamed branch, and — if there was a PR
// attached to the old branch — opens a replacement PR with the same base
// and body so the stack keeps its review thread. The old PR's number is
// replaced in gitcfg with the new one.
func recreateRemoteForRename(oldName, newName, newPath string, oldPR int) error {
	// Grab the old PR body BEFORE deleting the remote branch; gh will still
	// return it for a closed PR, but this way we don't depend on that.
	var oldBody string
	var oldBase string
	if oldPR > 0 {
		if body, err := gitx.Forge.GetPRBody(repo.PrimaryWorktree, oldPR); err == nil {
			oldBody = body
		}
		if base, err := gitx.Forge.GetPRBaseBranch(repo.PrimaryWorktree, oldPR); err == nil {
			oldBase = base
		}
	}

	if err := gitx.Remote.DeleteBranch(newPath, oldName); err != nil {
		return fmt.Errorf("deleting origin/%s: %w", oldName, err)
	}
	if err := gitx.Remote.Push(newPath, newName); err != nil {
		return fmt.Errorf("pushing %s to origin: %w", newName, err)
	}

	if oldPR == 0 {
		return nil
	}

	// Reload the (now-renamed) branch to resolve the base: prefer the old PR's
	// recorded base, fall back to the lineage parent, finally trunk.
	nb, err := state.LoadBranch(bare, newName)
	if err != nil {
		return err
	}
	base := oldBase
	if base == "" {
		base = nb.Parent
	}
	if base == "" {
		base = repo.Trunk
	}

	title := prTitleFor(newPath, base, newName)
	body := stripFooter(oldBody)
	url, num, err := gitx.Forge.CreatePR(forge.CreatePROpts{
		Worktree:   newPath,
		HeadBranch: newName,
		BaseBranch: base,
		Title:      title,
		Body:       body,
		IsDraft:    false,
	})
	if err != nil {
		return fmt.Errorf(
			"old PR #%d closed (branch renamed) but creating replacement failed: %w",
			oldPR, err,
		)
	}
	if err := state.UpdatePR(bare, newName, num); err != nil {
		return err
	}
	successf("PR #%d closed on rename", oldPR)
	successf("created PR #%d: %s <- %s", num, styleBranch(base), styleBranch(newName))
	hintf("view in browser @ %s", out.palette.Trunk.Render(url))
	return nil
}
