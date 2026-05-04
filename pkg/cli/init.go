package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/registry"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/spf13/cobra"
)

// git empty tree sha -> sha(1)
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

const initLongHelp = `
init a gg repo from three starting states:

  - empty directory       → creates .bare + empty initial commit + <trunk>/
  - directory with files  → same, plus relocates the existing files into
                            <trunk>/ where they sit as untracked content
  - regular git clone     → moves .git → .bare and relocates tracked +
                            untracked files into <trunk>/. refuses on a
                            dirty tree to avoid losing uncommitted work.

to clone an existing repo, use ` + "`" + `gg clone <url>` + "`" + `instead.`

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "init a gg-managed repo in the current directory.",
		Long:  initLongHelp,
		Args:  cobra.NoArgs,
		RunE:  runInit,
	}
	return cmd
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// already a bare repo: no-op
	if gitx.Revision.IsBareRepo(cwd) {
		return nil
	}
	if hasDotGit(cwd) {
		return convert(cwd)
	}
	return bootstrap(cwd)
}

func hasDotGit(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

// bootstraps a new gg managed repo
func bootstrap(cwd string) error {
	bareDir := filepath.Join(cwd, ".bare")
	if _, err := os.Stat(bareDir); err == nil {
		return fmt.Errorf(".bare already exists in %s", cwd)
	}
	trunk := configuredDefaultBranch(cwd)
	if err := refuseTrunkCollision(cwd, trunk); err != nil {
		return err
	}
	entries, err := fileEntries(cwd, ".bare")
	if err != nil {
		return err
	}
	hintf("initializing bare at %s", bareDir)
	if err := os.MkdirAll(bareDir, 0o755); err != nil {
		return err
	}
	if err := gitx.In(cwd).
		Cmd("init", "--bare", "--initial-branch="+trunk, bareDir).
		Err(); err != nil {
		return err
	}
	commitSHA, err := gitx.Commit.Tree(bareDir, emptyTreeSHA, "Initial commit", "")
	if err != nil {
		return fmt.Errorf("creating initial commit: %w", err)
	}
	if err := gitx.Ref.Update(bareDir, "refs/heads/"+trunk, commitSHA); err != nil {
		return err
	}
	primary := filepath.Join(cwd, trunk)
	if err := gitx.Worktree.Add(bareDir, primary, trunk); err != nil {
		return err
	}
	if err := moveEntries(cwd, primary, entries); err != nil {
		return fmt.Errorf("moving existing files into %s: %w", primary, err)
	}
	return finalize(cwd, bareDir, primary, trunk, primary, trunk)
}

// converts a normal git clone to a gg managed repo.
// refuses on dirty worktree or linked worktrees to avoid losing work.
func convert(cwd string) error {
	top, err := gitx.Revision.TopLevel(cwd)
	if err != nil {
		return err
	}
	top = filepath.Clean(top)
	gitDir := filepath.Join(top, ".git")
	info, err := os.Lstat(gitDir)
	if err != nil {
		return fmt.Errorf("no .git at %s", top)
	}
	if !info.IsDir() {
		return fmt.Errorf(
			"%s is a gitfile, not a directory; this dir is already a worktree of another repo",
			gitDir,
		)
	}
	if dirty, err := hasModifiedTracked(top); err != nil {
		return err
	} else if dirty {
		return fmt.Errorf(
			"working tree has uncommitted changes to tracked files; commit or stash first",
		)
	}
	wtList, err := gitx.Worktree.List(top)
	if err != nil {
		return err
	}
	if strings.Count(wtList, "\nworktree ") > 0 {
		return fmt.Errorf(
			"repo has linked worktrees; remove them with `git worktree remove` before converting",
		)
	}
	current, err := gitx.Revision.CurrentBranch(top)
	if err != nil {
		return err
	}
	if current == "HEAD" || current == "" {
		return fmt.Errorf("detached HEAD; checkout a branch before converting")
	}
	bareDir := filepath.Join(top, ".bare")
	if _, err := os.Stat(bareDir); err == nil {
		return fmt.Errorf(".bare already exists in %s", top)
	}
	// Snapshot user entries (tracked + untracked) before any rearrangement.
	// They'll all relocate to <trunk>/.
	entries, err := fileEntries(top, ".git", ".bare")
	if err != nil {
		return err
	}
	hintf("moving %s → %s", gitDir, bareDir)
	if err := os.Rename(gitDir, bareDir); err != nil {
		return fmt.Errorf("moving .git: %w", err)
	}
	// Rollback handler — restores .git and any user files we moved before
	// the failure point. Each step appends to undo and rollback runs in
	// reverse.
	undo := []func(){
		func() { _ = os.Rename(bareDir, gitDir) },
	}
	rollback := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	if err := gitx.Config.Set(bareDir, "core.bare", "true"); err != nil {
		rollback()
		return err
	}
	_ = gitx.Config.Unset(bareDir, "core.worktree")
	if err := gitx.Config.Set(
		bareDir,
		"remote.origin.fetch",
		"+refs/heads/*:refs/remotes/origin/*",
	); err != nil {
		rollback()
		return err
	}
	if err := gitx.Remote.FetchOrigin(bareDir); err == nil {
		_ = gitx.Remote.SetHeadAuto(bareDir, "origin")
	}
	trunk, err := gitx.Ref.DefaultBranch(bareDir)
	if err != nil || trunk == "" {
		trunk = current
	}
	_ = gitx.Ref.SetHead(bareDir, trunk)
	// The *active* worktree (where user files land + where we cd the shell
	// wrapper at the end) is the branch the user had checked out, which may
	// or may not be trunk. When current != trunk, we create a separate clean
	// trunk worktree afterward so both exist.
	activeBranch := current
	activePath := filepath.Join(top, activeBranch)
	// Refuse if a top-level user entry collides with either worktree dir we
	// plan to create.
	for _, name := range entries {
		if name == activeBranch || name == trunk {
			rollback()
			return fmt.Errorf(
				"existing entry %q in %s collides with the new %s/ worktree dir; rename it before trying again",
				name,
				top,
				name,
			)
		}
	}
	if err := os.Mkdir(activePath, 0o755); err != nil {
		rollback()
		return err
	}
	undo = append(undo, func() { _ = os.RemoveAll(activePath) })
	// Move the user's files into the active branch's worktree. Their content
	// matches that branch's tracked tree (dirty check already passed), so the
	// index we'll migrate next stays consistent.
	if err := moveEntries(top, activePath, entries); err != nil {
		rollback()
		return fmt.Errorf("moving user files into %s: %w", activePath, err)
	}
	undo = append(undo, func() {
		for _, name := range entries {
			_ = os.Rename(filepath.Join(activePath, name), filepath.Join(top, name))
		}
	})
	// Worktree metadata + gitfile for the active branch's worktree. The
	// migrated index (from the old .git, which was the active branch's) now
	// lives in this worktree's metadata — so git status agrees.
	activeMeta := filepath.Join(bareDir, "worktrees", activeBranch)
	if err := os.MkdirAll(activeMeta, 0o755); err != nil {
		rollback()
		return err
	}
	if err := writeWorktreeMeta(
		activeMeta,
		filepath.Join(activePath, ".git"),
		activeBranch,
	); err != nil {
		rollback()
		return err
	}
	if err := movePerWorktreeFiles(bareDir, activeMeta); err != nil {
		rollback()
		return fmt.Errorf("migrating per-worktree files: %w", err)
	}

	if _, err := gitx.In(activePath).Cmd("status", "--porcelain").String(); err != nil {
		return fmt.Errorf("verification failed after conversion: %w", err)
	}
	// If the user wasn't on trunk, create a clean trunk worktree alongside
	// via the normal git command so `git status` there is pristine from HEAD.
	trunkPath := activePath
	if activeBranch != trunk {
		trunkPath = filepath.Join(top, trunk)
		if err := gitx.Worktree.Add(bareDir, trunkPath, trunk); err != nil {
			return fmt.Errorf("adding trunk worktree: %w", err)
		}
	}
	err = finalize(top, bareDir, trunkPath, trunk, activePath, activeBranch)
	return err
}

// writes the gg config + registry entry
func finalize(cwd, bareDir, trunkPath, trunkName, activePath, activeBranchName string) error {
	cfg := state.Repo{
		Origin:          gitx.Remote.URL(bareDir, "origin"),
		Trunk:           trunkName,
		BareRepo:        bareDir,
		PrimaryWorktree: trunkPath,
		SyncStrategy:    "rebase",
		MergeStrategy:   "squash",
	}
	if err := state.SaveRepo(bareDir, cfg); err != nil {
		return err
	}
	if activeBranchName != trunkName {
		parentSHA, _ := gitx.Revision.HeadSHA(bareDir, trunkName)
		if err := state.SaveBranch(bareDir, state.Branch{
			Name:      activeBranchName,
			Parent:    trunkName,
			ParentSHA: parentSHA,
			Worktree:  activePath,
		}); err != nil {
			return err
		}
	}
	name := filepath.Base(cwd)
	if err := registry.Upsert(registry.Entry{
		Name: name, Bare: bareDir, PrimaryWorktree: trunkPath, Trunk: trunkName, Origin: cfg.Origin,
	}); err != nil {
		hintf("warning: could not update registry: %v", err)
	}
	successf("initialized %s (trunk: %s)\n", name, styleBranch(trunkName))
	plainln("  bare:     " + bareDir)
	plainln("  trunk: " + trunkPath)
	if activeBranchName != trunkName {
		plainln("  active:   " + activePath)
		stdout(activePath)
	} else {
		stdout(trunkPath)
	}
	return nil
}

// writeWorktreeMeta lays down git's per-worktree admin files at wtMeta and
// the gitfile that points the worktree at it. Used by both nested and flat
// conversion paths.
func writeWorktreeMeta(wtMeta, gitFilePath, branch string) error {
	write := func(name, content string) error {
		return os.WriteFile(filepath.Join(wtMeta, name), []byte(content), 0o644)
	}
	if err := write("HEAD", "ref: refs/heads/"+branch+"\n"); err != nil {
		return err
	}
	if err := write("commondir", "../..\n"); err != nil {
		return err
	}
	if err := write("gitdir", gitFilePath+"\n"); err != nil {
		return err
	}
	return os.WriteFile(gitFilePath, []byte("gitdir: "+wtMeta+"\n"), 0o644)
}

// refuseTrunkCollision errors out if cwd already contains an entry named
// trunk. Prevents `mkdir trunk` from clobbering the user's pre-existing dir.
func refuseTrunkCollision(cwd, trunk string) error {
	if _, err := os.Stat(filepath.Join(cwd, trunk)); err == nil {
		return fmt.Errorf(
			"entry %q already exists in %s and would collide with the primary worktree dir; rename it before trying again",
			trunk,
			cwd,
		)
	}
	return nil
}

// hasModifiedTracked returns true if the worktree has uncommitted edits to
// tracked files (staged or unstaged). Untracked files don't count — they
// move safely during conversion. The stricter `IsDirty` (which counts
// untracked as dirty) is used elsewhere where any non-clean state is unsafe.
func hasModifiedTracked(dir string) (bool, error) {
	out, err := gitx.In(dir).Cmd("status", "--porcelain", "--untracked-files=no").String()
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// configuredDefaultBranch reads init.defaultBranch from git config; falls
// back to "main" (the default in git 2.30+).
func configuredDefaultBranch(dir string) string {
	if out, err := gitx.In(dir).
		Cmd("config", "--get", "init.defaultBranch").
		String(); err == nil &&
		out != "" {
		return out
	}
	return "main"
}

func fileEntries(dir string, exclude ...string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if slices.Contains(exclude, e.Name()) {
			continue
		}
		out = append(out, e.Name())
	}
	return out, nil
}

// moveEntries renames each name under src into dst. On any failure, rolls
// back the moves it has already completed so the caller sees an all-or-
// nothing outcome.
func moveEntries(src, dst string, names []string) error {
	moved := make([]string, 0, len(names))
	for _, name := range names {
		from := filepath.Join(src, name)
		to := filepath.Join(dst, name)
		if err := os.Rename(from, to); err != nil {
			for _, m := range moved {
				_ = os.Rename(filepath.Join(dst, m), filepath.Join(src, m))
			}
			return fmt.Errorf("moving %s → %s: %w", from, to, err)
		}
		moved = append(moved, name)
	}
	return nil
}

// movePerWorktreeFiles relocates state git expects under the worktree
// metadata dir after an in-place conversion. Shared by both layouts.
func movePerWorktreeFiles(bareDir, wtMeta string) error {
	candidates := []string{
		"index",
		"FETCH_HEAD",
		"ORIG_HEAD",
		"MERGE_HEAD",
		"MERGE_MODE",
		"MERGE_MSG",
		"CHERRY_PICK_HEAD",
		"REVERT_HEAD",
		"logs/HEAD",
	}
	for _, rel := range candidates {
		src := filepath.Join(bareDir, rel)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(wtMeta, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}
	return nil
}
