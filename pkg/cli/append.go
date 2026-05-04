package cli

import (
	"fmt"
	"strings"

	"github.com/ashi-labs/gg/pkg/config"
	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/spf13/cobra"
)

func newAppendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "append <branch>",
		Aliases: []string{"a"},
		Short:   "append a new branch + worktree as a child of the current branch.",
		Args:    cobra.ExactArgs(1),
		RunE:    runAppend,
	}
	cmd.Flags().
		BoolP("all", "a", false, "stage all changes in the current worktree and commit them on the new branch")
	cmd.Flags().StringP("message", "m", "", "commit message (required with --all)")
	return cmd
}

func runAppend(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	branch := args[0]
	current, err := gitx.Revision.CurrentBranch(cwd)
	if err != nil {
		return err
	}
	parent := current
	if current == "HEAD" || current == "" {
		parent = repo.Trunk
	}
	opts, err := newBranchOptsFrom(cmd)
	if err != nil {
		return err
	}
	return appendBranchWorktree(branch, parent, opts)
}

// newBranchOpts carries the shared --all/--message flags for `gg new` and
// `gg append`. Consumed by appendBranchWorktree.
type newBranchOpts struct {
	all     bool
	message string
}

func newBranchOptsFrom(cmd *cobra.Command) (newBranchOpts, error) {
	all, _ := cmd.Flags().GetBool("all")
	msg, _ := cmd.Flags().GetString("message")
	if all && msg == "" {
		return newBranchOpts{}, fmt.Errorf("--all requires -m/--message")
	}
	return newBranchOpts{all: all, message: msg}, nil
}

// appendBranchWorktree is the shared body of `gg append` and `gg new`: make
// a new branch + its worktree, register lineage, and print the path to stdout
// for the shell wrapper.
func appendBranchWorktree(branch, parent string, opts newBranchOpts) error {
	path := repo.WorktreePath(branch)
	parentSHA, err := gitx.Revision.HeadSHA(bare, parent)
	if err != nil {
		return fmt.Errorf("could not resolve parent %q: %w", parent, err)
	}
	// Pre-check: if refs/heads/<branch> already exists in the bare (left over
	// from a prior workflow), `git worktree add -b` fails with a terse
	// "branch already exists" message. Surface remediation hints instead.
	if gitx.Branch.HasLocal(bare, branch) {
		return fmt.Errorf(
			"a git branch named %q already exists but isn't tracked by gg. To resolve:\n"+
				"  - drop it:  git -C %s branch -D %s\n"+
				"  - adopt it: check it out in a worktree, then run `gg track`",
			branch, bare, branch,
		)
	}

	// Each worktree has its own index, so we can't rely on the parent's
	// staged state being visible in the new worktree. We stash in the parent
	// (scoped by --all vs -m), create the new worktree, then pop there and
	// commit.
	//   --all: stash everything including untracked → commit everything.
	//   -m alone: stash only the index (--staged) → commit only what was staged;
	//             unstaged changes in the parent worktree are untouched.
	stashed := false
	switch {
	case opts.all:
		dirty, err := gitx.Status.IsDirty(cwd)
		if err != nil {
			return err
		}
		if !dirty {
			return fmt.Errorf("--all: no changes to commit in %s", cwd)
		}
		if err := gitx.In(cwd).
			Cmd("stash", "push", "--include-untracked", "-m", "gg-auto").
			Err(); err != nil {
			return err
		}
		stashed = true
	case opts.message != "":
		staged, err := gitx.Status.HasStaged(cwd)
		if err != nil {
			return err
		}
		if !staged {
			return fmt.Errorf(
				"-m: nothing staged in %s (use --all to include unstaged changes)",
				cwd,
			)
		}
		if err := gitx.Stash.PushStaged(cwd, "gg-auto"); err != nil {
			return err
		}
		stashed = true
	}
	if err := gitx.Worktree.AddWithBranch(bare, path, branch, parent); err != nil {
		if stashed {
			_ = gitx.Stash.Pop(cwd)
		}
		return err
	}

	b := state.Branch{
		Name:      branch,
		Parent:    parent,
		ParentSHA: parentSHA,
		Worktree:  path,
	}
	if err := state.SaveBranch(bare, b); err != nil {
		return err
	}

	// Set up upstream tracking so `git pull` / status-vs-origin works without
	// the user running `--set-upstream-to` themselves. Safe to do before any
	// push: we're just writing branch.<name>.{remote,merge} config.
	if gitx.Remote.Exists(bare, "origin") {
		if err := gitx.Remote.SetUpstreamConfig(path, branch); err != nil {
			return fmt.Errorf("setting upstream for %s: %w", branch, err)
		}
	}

	if stashed {
		if err := gitx.Stash.Pop(path); err != nil {
			return fmt.Errorf(
				"popping stash into %s: %w (your changes are still in `git stash list`)",
				path, err,
			)
		}
		if err := gitx.Index.AddAll(path); err != nil {
			return err
		}
		if err := gitx.Commit.Create(path, opts.message); err != nil {
			return err
		}
	}

	successf("created %s  (off %s)", styleBranch(branch), styleBranch(parent))
	seedFromParent(parent, path)
	stdout(path)
	return nil
}

// seedFromParent copies user-configured ignored paths (node_modules,
// .env, ...) from the parent branch's worktree into the freshly-created
// child. Strictly best-effort: a missing parent worktree, an unknown
// parent branch, or a failed clone all short-circuit silently. The
// success line is printed only when at least one path was seeded so the
// common case (repo has no seeds installed yet) stays quiet.
func seedFromParent(parent, dst string) {
	paths := config.Load().Seed.Paths
	if len(paths) == 0 {
		return
	}
	src := parentWorktreePath(parent)
	if src == "" {
		return
	}
	res := gitx.Worktree.Seed(src, dst, paths)
	if len(res.Seeded) > 0 {
		hintf("seeded %s from %s", strings.Join(res.Seeded, ", "), styleBranch(parent))
	}
	if len(res.Skipped) > 0 {
		hintf(
			"could not seed %s (filesystem doesn't support clone or hardlink across it)",
			strings.Join(res.Skipped, ", "),
		)
	}
}

// parentWorktreePath resolves the on-disk worktree for a parent branch.
// Trunk lives at the primary worktree; everything else is looked up in
// gitcfg. Returns "" when we can't find a live source to seed from.
func parentWorktreePath(parent string) string {
	if parent == repo.Trunk {
		return repo.PrimaryWorktree
	}
	b, err := state.LoadBranch(bare, parent)
	if err != nil {
		return ""
	}
	return b.Worktree
}
