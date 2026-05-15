package sync

import (
	"fmt"
	"os"
	"time"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
)

// artificial delay for testing
func delay() {
	if os.Getenv("DEBUG") == "1" {
		time.Sleep(1 * time.Second)
	}
}

type RunOpts struct {
	NoFetch bool
	// Scope optionally restricts the rebase plan. Empty Scope means "every
	// branch in the repo" (whole-repo sync). Pruning and footer refresh
	// run globally regardless of Scope — they're handled outside the
	// engine via OnEvent.
	Scope Scope
	// OnEvent receives every progress event the engine emits. Returning a
	// non-nil error aborts the run — handlers use this to inject work
	// between phases (e.g. pruning forge-merged branches on EventFetchDone
	// before the rebase plan is built) and to surface failures in that
	// work back to the engine. nil-safe: a missing callback is replaced
	// with a no-op.
	OnEvent func(Event) error
}

// Scope narrows which branches the engine includes in the rebase plan.
// Zero-valued Scope means whole repo.
type Scope struct {
	// StackOf, if set, restricts the plan to the stack containing this
	// branch (same semantics as stack.Lineage.StackOf — the connected
	// subtree rooted at a child of trunk). Branches outside that subtree
	// are skipped. If the named branch is trunk or untracked, the plan is
	// empty.
	StackOf string
}

type EventKind int

const (
	EventPlan          EventKind = iota // fired once before any work with ordered list of branches to rebase
	EventFetchStart                     // fires before fetch + fast forward of trunk if fetch enabled
	EventFetchDone                      // fires after fetch + fast forward of trunk if fetch enabled
	EventBranchStart                    // fires before `git rebase`
	EventBranchDone                     // fires after a clean rebase
	EventBranchSkipped                  // fires when the parent hasn't changed since last sync
	EventBranchFailed                   // fires on rebase conflict; persists runstate for `gg abort/continue`
	EventBranchPruned                   // fires when OnPostFetch removes a branch (e.g. PR merged on the forge)
)

type Event struct {
	Kind     EventKind
	Branch   string   // populated for per-branch events
	Branches []string // populated for EventPlan (in topological order)
	Err      error    // populated for EventBranchFailed
	Detail   string   // free-form annotation (e.g. "PR #13 merged" for EventBranchPruned)
}

// performs a sync (fetch + ff + cascading restack) or just a restack
// refuses if a previous sync needs `gg abort/continue`
func Run(repo *state.Repo, bare string, opts RunOpts) error {
	emit := opts.OnEvent
	if emit == nil {
		emit = func(Event) error { return nil }
	}
	if rs, _ := Load(bare); rs != nil {
		return fmt.Errorf(
			"sync already in progress (paused on %s); use `gg continue` or `gg abort`",
			rs.InProgressBranch,
		)
	}
	// Pre-fetch cleanliness check: only the primary worktree matters here,
	// because that's the one fast-forwarded by fetchAndFastForwardTrunk.
	// Skip entirely when NoFetch — no ff means trunk's worktree isn't
	// touched by this code path.
	if !opts.NoFetch {
		if err := ensurePrimaryClean(repo); err != nil {
			return err
		}
		if err := emit(Event{Kind: EventFetchStart}); err != nil {
			return err
		}
		delay() // fetch is usually fast — gives the fetch spinner air
		if err := fetchAndFastForwardTrunk(repo, bare); err != nil {
			return err
		}
	}
	// EventFetchDone fires unconditionally — even with NoFetch=true — so
	// handlers that want to inject post-fetch work (notably the CLI's
	// merged-PR prune) have a single, reliable hook point. The engine
	// then re-loads branches in case the handler mutated state, before
	// computing the rebase plan.
	if err := emit(Event{Kind: EventFetchDone}); err != nil {
		return err
	}
	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}
	l := stack.Build(repo.Trunk, branches)
	order := scopedOrder(l, opts.Scope)
	if err := emit(Event{Kind: EventPlan, Branches: order}); err != nil {
		return err
	}
	if len(order) == 0 {
		return nil
	}
	// Pre-rebase cleanliness check: only the worktrees we're about to
	// rebase need to be clean. Sibling stacks (out of scope) can be as
	// dirty as they like — they're not getting touched. This is the
	// fix for the bug where a stack-scoped sync refused because
	// uninvolved branches' worktrees had uncommitted edits.
	if err := ensureClean(branchesByName(branches, order)); err != nil {
		return err
	}
	snapshots, err := snapshotHeads(repo, bare, order)
	if err != nil {
		return err
	}
	rs := &RunState{
		Kind:      kindFor(opts),
		Trunk:     repo.Trunk,
		Scope:     opts.Scope,
		Snapshots: snapshots,
		Remaining: order,
	}
	return runPlan(repo, bare, rs, emit)
}

// Continue resumes a paused sync after the user resolved conflicts in
// the paused worktree and staged the fixed files with `git add`. It
// drives `git rebase --continue` itself so the user doesn't need to.
// Accepts the same Opts as Run so the CLI can plumb through its
// progress callback.
func Continue(repo *state.Repo, bare string, opts RunOpts) error {
	emit := opts.OnEvent
	if emit == nil {
		emit = func(Event) error { return nil }
	}
	rs, err := Load(bare)
	if err != nil {
		return err
	}
	if rs == nil {
		return fmt.Errorf("nothing to continue; no sync in progress")
	}
	if rs.InProgressBranch == "" || rs.InProgressWorktree == "" {
		return fmt.Errorf("malformed runstate (missing in-progress branch)")
	}
	inFlight, err := gitx.Rebase.InProgress(rs.InProgressWorktree)
	if err != nil {
		return err
	}
	if inFlight {
		// Drive the paused rebase forward ourselves. We pin core.editor
		// to `true` (the unix command) so git doesn't pop an editor for
		// the commit message — rebases in a stack workflow are
		// mechanical; matching the original message is what we want.
		// If conflicts remain, git --continue exits non-zero and we
		// bubble that up with a clearer hint.
		if err := gitx.Rebase.Continue(rs.InProgressWorktree); err != nil {
			return fmt.Errorf(
				"rebase in %s still has unresolved conflicts — fix files, `git add` them, then rerun `gg continue` (%w)",
				rs.InProgressWorktree,
				err,
			)
		}
		// Sanity-check: if git still reports the rebase as in-progress
		// the continue must have paused on a later commit in the range.
		if stillIn, _ := gitx.Rebase.InProgress(rs.InProgressWorktree); stillIn {
			return fmt.Errorf(
				"rebase in %s paused on another commit — resolve, then rerun `gg continue`",
				rs.InProgressWorktree,
			)
		}
	}

	// The paused branch's own rebase finished. Record its new parent-sha.
	b, err := state.LoadBranch(bare, rs.InProgressBranch)
	if err != nil {
		return err
	}
	parent := b.Parent
	if parent == "" {
		parent = repo.Trunk
	}
	parentNow, err := gitx.Revision.HeadSHA(bare, parent)
	if err != nil {
		return err
	}
	if err := state.UpdateParentSHA(bare, rs.InProgressBranch, parentNow); err != nil {
		return err
	}

	// The resolved branch counts as "done" from the progress UI's POV.
	if err := emit(Event{Kind: EventBranchDone, Branch: rs.InProgressBranch}); err != nil {
		return err
	}

	// Drop the resolved branch from Remaining and keep going.
	if len(rs.Remaining) > 0 && rs.Remaining[0] == rs.InProgressBranch {
		rs.Remaining = rs.Remaining[1:]
	}
	// Report the still-remaining work so the UI pre-populates rows.
	if len(rs.Remaining) > 0 {
		if err := emit(
			Event{Kind: EventPlan, Branches: append([]string(nil), rs.Remaining...)},
		); err != nil {
			return err
		}
	}
	rs.InProgressBranch = ""
	rs.InProgressWorktree = ""
	return runPlan(repo, bare, rs, emit)
}

// Abort aborts any in-flight rebase and resets every snapshotted branch to
// its pre-sync SHA. Used to undo a partial sync.
func Abort(repo *state.Repo, bare string) error {
	rs, err := Load(bare)
	if err != nil {
		return err
	}
	if rs == nil {
		return fmt.Errorf("nothing to abort; no sync in progress")
	}
	if rs.InProgressWorktree != "" {
		_ = gitx.Rebase.Abort(rs.InProgressWorktree)
	}
	for name, sha := range rs.Snapshots {
		wt := repo.PrimaryWorktree
		if name != repo.Trunk {
			b, err := state.LoadBranch(bare, name)
			if err != nil || b.Worktree == "" {
				continue
			}
			wt = b.Worktree
		}
		_ = gitx.Reset.Hard(wt, sha)
	}
	return Clear(bare)
}

// runPlan iterates rs.Remaining, rebasing each branch onto its parent. On
// failure (typically a merge conflict) it persists runstate and returns a
// hint pointing the user at the paused worktree. The emit callback
// receives per-branch progress events.
func runPlan(repo *state.Repo, bare string, rs *RunState, emit func(Event) error) error {
	for len(rs.Remaining) > 0 {
		name := rs.Remaining[0]
		b, err := state.LoadBranch(bare, name)
		if err != nil {
			return err
		}
		if b.Parent == "" {
			delay() // pace out otherwise-instant skips
			if err := emit(Event{Kind: EventBranchSkipped, Branch: name}); err != nil {
				return err
			}
			rs.Remaining = rs.Remaining[1:]
			continue
		}
		parentNow, err := gitx.Revision.HeadSHA(bare, b.Parent)
		if err != nil {
			return err
		}
		parentOld := b.ParentSHA
		if parentOld == "" {
			parentOld = parentNow
		}
		if parentNow == parentOld {
			// Parent hasn't moved since last sync — nothing to replay.
			delay()
			if err := emit(Event{Kind: EventBranchSkipped, Branch: name}); err != nil {
				return err
			}
			rs.Remaining = rs.Remaining[1:]
			continue
		}
		if b.Worktree == "" {
			return fmt.Errorf("no worktree recorded for %s; cannot rebase", name)
		}
		if err := emit(Event{Kind: EventBranchStart, Branch: name}); err != nil {
			return err
		}
		delay() // lets the spinner render for the full pause duration
		if err := gitx.Rebase.Onto(b.Worktree, parentNow, parentOld); err != nil {
			rs.InProgressBranch = name
			rs.InProgressWorktree = b.Worktree
			_ = emit(Event{Kind: EventBranchFailed, Branch: name, Err: err})
			if saveErr := Save(bare, rs); saveErr != nil {
				return fmt.Errorf(
					"rebase of %s failed AND saving runstate failed: %v / %w",
					name,
					err,
					saveErr,
				)
			}
			return fmt.Errorf(
				"rebase of %s paused: resolve conflicts in %s, `git add` the files, then run `gg continue`",
				name,
				b.Worktree,
			)
		}
		if err := state.UpdateParentSHA(bare, name, parentNow); err != nil {
			return err
		}
		if err := emit(Event{Kind: EventBranchDone, Branch: name}); err != nil {
			return err
		}
		rs.Remaining = rs.Remaining[1:]
	}
	return Clear(bare)
}

// scopedOrder returns the topological rebase plan, optionally filtered to a
// single stack. With an empty Scope it's just l.Topological(). With
// Scope.StackOf set, branches outside that stack are dropped while parent-
// before-child order is preserved.
func scopedOrder(l stack.Lineage, scope Scope) []string {
	order := l.Topological()
	if scope.StackOf == "" {
		return order
	}
	stackBranches := l.StackOf(scope.StackOf)
	if len(stackBranches) == 0 {
		return nil
	}
	keep := make(map[string]bool, len(stackBranches))
	for _, n := range stackBranches {
		keep[n] = true
	}
	filtered := make([]string, 0, len(stackBranches))
	for _, n := range order {
		if keep[n] {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

// ensurePrimaryClean fails fast when the primary worktree (where trunk
// lives) has uncommitted changes — `git merge --ff-only` would either
// refuse or land confusingly mid-fetch otherwise.
func ensurePrimaryClean(repo *state.Repo) error {
	if repo.PrimaryWorktree == "" {
		return nil
	}
	dirty, err := gitx.Status.IsDirty(repo.PrimaryWorktree)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("primary worktree has uncommitted changes; commit or stash first")
	}
	return nil
}

// ensureClean fails when any of the given branches' worktrees has
// uncommitted changes. Caller is responsible for filtering the input
// to in-scope branches — passing the full set would (incorrectly)
// block a stack-scoped sync over dirty siblings.
func ensureClean(branches []state.Branch) error {
	for _, b := range branches {
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
				b.Name,
				b.Worktree,
			)
		}
	}
	return nil
}

// branchesByName returns the subset of `all` whose Name appears in
// `names`, preserving names ordering. Used to project an in-scope
// rebase plan back to the full Branch records (with Worktree paths).
func branchesByName(all []state.Branch, names []string) []state.Branch {
	if len(names) == 0 {
		return nil
	}
	byName := make(map[string]state.Branch, len(all))
	for _, b := range all {
		byName[b.Name] = b
	}
	out := make([]state.Branch, 0, len(names))
	for _, n := range names {
		if b, ok := byName[n]; ok {
			out = append(out, b)
		}
	}
	return out
}

func fetchAndFastForwardTrunk(repo *state.Repo, bare string) error {
	if !gitx.Remote.Exists(bare, "origin") {
		// No remote configured — nothing to fetch from. This is valid for
		// repos bootstrapped via `gg init` in an empty dir.
		return nil
	}
	if err := gitx.Remote.FetchOrigin(bare); err != nil {
		return fmt.Errorf("fetch origin: %w", err)
	}
	if err := gitx.In(repo.PrimaryWorktree).
		Cmd("merge", "--ff-only", "origin/"+repo.Trunk).
		Err(); err != nil {
		return fmt.Errorf(
			"fast-forward %s from origin: %w (local trunk has diverged)",
			repo.Trunk,
			err,
		)
	}
	return nil
}

func snapshotHeads(repo *state.Repo, bare string, order []string) (map[string]string, error) {
	snapshots := map[string]string{}
	if sha, err := gitx.Revision.HeadSHA(bare, repo.Trunk); err == nil {
		snapshots[repo.Trunk] = sha
	}
	for _, n := range order {
		sha, err := gitx.Revision.HeadSHA(bare, n)
		if err != nil {
			return nil, fmt.Errorf("reading head for %s: %w", n, err)
		}
		snapshots[n] = sha
	}
	return snapshots, nil
}

func kindFor(opts RunOpts) string {
	if opts.NoFetch {
		return "restack"
	}
	return "sync"
}
