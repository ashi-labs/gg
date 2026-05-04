package cli

import (
	"fmt"
	"os/exec"
	"sort"

	"github.com/ashi-labs/gg/pkg/config"
	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/gitx/forge"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/sync"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sync",
		Aliases: []string{"s"},
		Short:   "Fetch trunk from origin and restack every branch in the repo.",
		Args:    cobra.NoArgs,
		RunE:    runSync,
	}
	cmd.Flags().Bool("no-fetch", false, "skip fetching from origin")
	return cmd
}

func runSync(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	noFetch, _ := cmd.Flags().GetBool("no-fetch")

	// hop captures the worktree the shell wrapper should `cd` into when
	// the user was standing in a now-pruned merged branch. Populated by
	// the preRebase callback below; emitted to stdout post-Run.
	var hop string
	title := "syncing " + repo.ShortName()
	preRebase := func(emit func(sync.Event) error, suspend ttySuspender) error {
		h, err := preSyncPrune(emit, suspend)
		hop = h
		return err
	}
	if err := runSyncWithProgress(sync.RunOpts{NoFetch: noFetch}, title, preRebase); err != nil {
		return err
	}

	// Footer refresh stays post-sync so renames/lineage shifts that
	// happened during the rebase get reflected in the PR bodies.
	if err := refreshOpenPRFooters(); err != nil {
		return err
	}

	if hop != "" {
		stdout(hop)
	}
	return nil
}

// preSyncPrune detects PRs that were merged on the forge and removes their
// local branches. Called by the progress UI's OnEvent handler on
// EventFetchDone — that's the engine hook point between fetch/FF and the
// rebase plan, which is exactly when we need to drop merged branches so
// the rebase loop never sees them. Otherwise replaying a merged branch's
// commits onto a trunk that already contains them reliably conflicts.
//
// Children are reparented onto the merged branch's parent with the merged
// branch's current tip as their new parent-sha, so the next rebase replays
// only the child-specific commits onto the grandparent.
//
// Returns a target worktree path if the user was standing in a pruned
// branch's worktree; runSync emits it on stdout so the shell wrapper can
// `cd` out before the now-deleted directory strands the shell.
//
// Best-effort: missing `gh` or a misconfigured remote skips the whole
// step. Any PR fetch failure is logged but does not fail the sync.
func preSyncPrune(emit func(sync.Event) error, suspend ttySuspender) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", nil
	}
	if !gitx.Remote.Exists(bare, "origin") {
		return "", nil
	}
	return pruneMergedBranches(emit, suspend)
}

// refreshOpenPRFooters rewrites the stack footer on every remaining PR
// body once the sync has settled, so lineage-tree comments reflect any
// renames / deletions that happened during the run. Best-effort same as
// preSyncPrune — gh + origin gating, per-PR errors logged.
func refreshOpenPRFooters() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil
	}
	if !gitx.Remote.Exists(bare, "origin") {
		return nil
	}
	return refreshPRFooters()
}

// pruneMergedBranches removes any tracked branch whose PR is in the MERGED
// state on GitHub. Children are reparented onto the merged branch's parent
// with the merged branch's current tip as their new parent-sha — so the next
// sync replays only the child-specific commits onto the grandparent.
//
// If the user is currently standing in a merged branch's worktree, we can't
// delete it from underneath them — but we also don't want to bail. Instead,
// pick a sensible neighbor (first child if any, else parent) and return its
// worktree path so `runSync` can emit it on stdout for the shell wrapper to
// `cd` into after the command exits.
func pruneMergedBranches(emit func(sync.Event) error, suspend ttySuspender) (string, error) {
	branches, err := state.AllBranches(bare)
	if err != nil {
		return "", err
	}
	l := stack.Build(repo.Trunk, branches)
	// Read the on-disk PR-status cache once. Only cached MERGED entries
	// are reused — PRs can't be un-merged, so those are always safe.
	// Anything else (OPEN/CLOSED/missing) gets a live fetch: a precmd
	// prefetch from before the merge landed would otherwise pin a stale
	// OPEN here and skip the prune entirely. Sync is the moment we
	// most need ground truth from the forge.
	cached := loadFreshPRStatuses(config.Load(), false)
	var merged []state.Branch
	resolved := make(map[string]forge.PRStatus, len(branches))
	for _, b := range branches {
		if b.PRNumber == 0 {
			continue
		}
		var status forge.PRStatus
		if s, ok := cached[b.Name]; ok && s.State == "MERGED" {
			status = s
		} else {
			s, err := gitx.Forge.GetPRStatus(repo.PrimaryWorktree, b.PRNumber)
			if err != nil {
				errorf("checking PR #%d for %s: %v", b.PRNumber, styleBranch(b.Name), err)
				continue
			}
			status = s
		}
		resolved[b.Name] = status
		if status.State == "MERGED" {
			merged = append(merged, b)
		}
	}
	// Fold the resolved set back into the cache so subsequent callers
	// (the next `gg log`, another `gg sync`) see fresh entries even if
	// some came from live fetches we just performed.
	writePRCacheFromModel(branches, resolved)
	if len(merged) == 0 {
		return "", nil
	}
	current, _ := gitx.Revision.CurrentBranch(cwd)
	mergedSet := make(map[string]bool, len(merged))
	for _, b := range merged {
		mergedSet[b.Name] = true
	}
	// If the user is on a merged branch, pre-compute where they should land
	// once its worktree is gone. Prefer the first child (so they stay
	// downstream in the same stack); fall back to the parent. Resolve to a
	// concrete path now — after removeBranch, the lineage entry is gone.
	hopTarget := ""
	if mergedSet[current] {
		cur := l.ByName[current]
		kids := l.Children(current)
		sort.Strings(kids)
		var dest string
		if len(kids) > 0 {
			dest = kids[0]
		} else {
			dest = cur.Parent
		}
		switch dest {
		case "", repo.Trunk:
			hopTarget = repo.PrimaryWorktree
		default:
			if b, ok := l.ByName[dest]; ok {
				hopTarget = b.Worktree
			}
		}
	}
	// Remove leaf-first so a merged branch's still-local children get
	// reparented onto its parent before the branch itself disappears.
	topo := l.Topological()
	for i := len(topo) - 1; i >= 0; i-- {
		name := topo[i]
		if !mergedSet[name] {
			continue
		}
		b := l.ByName[name]
		mergedTip, _ := gitx.Revision.HeadSHA(bare, name)
		for _, kid := range l.Children(name) {
			if err := state.UpdateParent(bare, kid, b.Parent); err != nil {
				return "", err
			}
			if mergedTip != "" {
				_ = state.UpdateParentSHA(bare, kid, mergedTip)
			}
		}
		// Confirm per branch in the order we reach them. Wrap in
		// suspend so huh isn't fighting bubbletea for stdin (otherwise
		// the user has to press Enter twice).
		var rmErr error
		if err := suspend(func() error {
			rmErr = removeBranch(b, false, false)
			return nil
		}); err != nil {
			return "", err
		}
		if rmErr != nil {
			errorf("cleaning up merged branch %s: %v", styleBranch(name), rmErr)
			continue
		}
		// Emit through the engine's event stream so the progress UI
		// renders the pruned line in the same frame as the surrounding
		// fetch/rebase rows. Detail carries the PR-number annotation
		// since the engine itself doesn't know about forge concepts.
		if emit != nil {
			_ = emit(sync.Event{
				Kind:   sync.EventBranchPruned,
				Branch: name,
				Detail: fmt.Sprintf("PR #%d merged", b.PRNumber),
			})
		}
	}
	return hopTarget, nil
}

// refreshPRFooters rewrites the stack footer on every open PR so names (after
// rename) and tree shape (after merges/deletes) stay in sync with local
// lineage. No-op for PRs whose body would be unchanged.
func refreshPRFooters() error {
	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}
	lineage := stack.Build(repo.Trunk, branches)
	prs := map[string]int{}
	for _, b := range branches {
		if b.PRNumber > 0 {
			prs[b.Name] = b.PRNumber
		}
	}
	if len(prs) == 0 {
		return nil
	}
	for name, num := range prs {
		body, err := gitx.Forge.GetPRBody(repo.PrimaryWorktree, num)
		if err != nil {
			errorf("reading PR #%d body: %v", num, err)
			continue
		}
		updated := withUpdatedFooter(body, name, repo.Trunk, lineage, prs)
		if updated == body {
			continue
		}
		if err := gitx.Forge.EditPRBody(repo.PrimaryWorktree, num, updated); err != nil {
			errorf("updating PR #%d body: %v", num, err)
		}
	}
	return nil
}
