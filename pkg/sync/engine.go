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

type Repo struct {
	BareDir         string
	Trunk           string
	PrimaryWorktree string
}

type RunOpts struct {
	NoFetch bool
	// OnEvent receives every progress event the engine emits. Returning a
	// non-nil error aborts the run — handlers use this to inject work
	// between phases (e.g. pruning forge-merged branches on EventFetchDone
	// before the rebase plan is built) and to surface failures in that
	// work back to the engine. nil-safe: a missing callback is replaced
	// with a no-op.
	OnEvent func(Event) error
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
func Run(repo Repo, opts RunOpts) error {
	emit := opts.OnEvent
	if emit == nil {
		emit = func(Event) error { return nil }
	}
	if rs, _ := Load(repo.BareDir); rs != nil {
		return fmt.Errorf(
			"sync already in progress (paused on %s); use `gg continue` or `gg abort`",
			rs.InProgressBranch,
		)
	}
	branches, err := state.AllBranches(repo.BareDir)
	if err != nil {
		return err
	}
	if err := ensureClean(repo, branches); err != nil {
		return err
	}
	if !opts.NoFetch {
		if err := emit(Event{Kind: EventFetchStart}); err != nil {
			return err
		}
		delay() // fetch is usually fast — gives the fetch spinner air
		if err := fetchAndFastForwardTrunk(repo); err != nil {
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
	branches, err = state.AllBranches(repo.BareDir)
	if err != nil {
		return err
	}
	l := stack.Build(repo.Trunk, branches)
	order := l.Topological()
	if err := emit(Event{Kind: EventPlan, Branches: order}); err != nil {
		return err
	}
	if len(order) == 0 {
		return nil
	}
	snapshots, err := snapshotHeads(repo, order)
	if err != nil {
		return err
	}
	rs := &RunState{
		Kind:      kindFor(opts),
		Trunk:     repo.Trunk,
		Snapshots: snapshots,
		Remaining: order,
	}
	return runPlan(repo, rs, emit)
}

// Continue resumes a paused sync after the user resolved conflicts in
// the paused worktree and staged the fixed files with `git add`. It
// drives `git rebase --continue` itself so the user doesn't need to.
// Accepts the same Opts as Run so the CLI can plumb through its
// progress callback.
func Continue(repo Repo, opts RunOpts) error {
	emit := opts.OnEvent
	if emit == nil {
		emit = func(Event) error { return nil }
	}
	rs, err := Load(repo.BareDir)
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
	b, err := state.LoadBranch(repo.BareDir, rs.InProgressBranch)
	if err != nil {
		return err
	}
	parent := b.Parent
	if parent == "" {
		parent = repo.Trunk
	}
	parentNow, err := gitx.Revision.HeadSHA(repo.BareDir, parent)
	if err != nil {
		return err
	}
	if err := state.UpdateParentSHA(repo.BareDir, rs.InProgressBranch, parentNow); err != nil {
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
		if err := emit(Event{Kind: EventPlan, Branches: append([]string(nil), rs.Remaining...)}); err != nil {
			return err
		}
	}
	rs.InProgressBranch = ""
	rs.InProgressWorktree = ""
	return runPlan(repo, rs, emit)
}

// Abort aborts any in-flight rebase and resets every snapshotted branch to
// its pre-sync SHA. Used to undo a partial sync.
func Abort(repo Repo) error {
	rs, err := Load(repo.BareDir)
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
			b, err := state.LoadBranch(repo.BareDir, name)
			if err != nil || b.Worktree == "" {
				continue
			}
			wt = b.Worktree
		}
		_ = gitx.Reset.Hard(wt, sha)
	}
	return Clear(repo.BareDir)
}

// runPlan iterates rs.Remaining, rebasing each branch onto its parent. On
// failure (typically a merge conflict) it persists runstate and returns a
// hint pointing the user at the paused worktree. The emit callback
// receives per-branch progress events.
func runPlan(repo Repo, rs *RunState, emit func(Event) error) error {
	for len(rs.Remaining) > 0 {
		name := rs.Remaining[0]
		b, err := state.LoadBranch(repo.BareDir, name)
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
		parentNow, err := gitx.Revision.HeadSHA(repo.BareDir, b.Parent)
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
			if saveErr := Save(repo.BareDir, rs); saveErr != nil {
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
		if err := state.UpdateParentSHA(repo.BareDir, name, parentNow); err != nil {
			return err
		}
		if err := emit(Event{Kind: EventBranchDone, Branch: name}); err != nil {
			return err
		}
		rs.Remaining = rs.Remaining[1:]
	}
	return Clear(repo.BareDir)
}

func ensureClean(repo Repo, branches []state.Branch) error {
	worktree := repo.PrimaryWorktree
	if worktree != "" {
		if dirty, err := gitx.Status.IsDirty(worktree); err != nil {
			return err
		} else if dirty {
			return fmt.Errorf("primary worktree has uncommitted changes; commit or stash first")
		}
	}
	for _, b := range branches {
		if b.Worktree == "" {
			continue
		}
		if dirty, err := gitx.Status.IsDirty(b.Worktree); err != nil {
			return err
		} else if dirty {
			return fmt.Errorf(
				"%s has uncommitted changes in %s; commit or stash first",
				b.Name,
				b.Worktree,
			)
		}
	}
	return nil
}

func fetchAndFastForwardTrunk(repo Repo) error {
	if !gitx.Remote.Exists(repo.BareDir, "origin") {
		// No remote configured — nothing to fetch from. This is valid for
		// repos bootstrapped via `gg init` in an empty dir.
		return nil
	}
	if err := gitx.Remote.FetchOrigin(repo.BareDir); err != nil {
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

func snapshotHeads(repo Repo, order []string) (map[string]string, error) {
	snapshots := map[string]string{}
	if sha, err := gitx.Revision.HeadSHA(repo.BareDir, repo.Trunk); err == nil {
		snapshots[repo.Trunk] = sha
	}
	for _, n := range order {
		sha, err := gitx.Revision.HeadSHA(repo.BareDir, n)
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
