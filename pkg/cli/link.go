package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/registry"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/spf13/cobra"
)

func newLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "link [bare-path]",
		Aliases: []string{"ln"},
		Short:   "Re-bind a moved gg repo: rewrite stale paths in its config and the registry.",
		Long: `Run from inside a gg-managed repo that has been moved on disk. gg link
detects the new bare location (or takes one as an argument), runs
` + "`git worktree repair`" + ` to fix git's internal pointers, then rewrites the
paths in the bare's gg config (primary worktree + every tracked branch's
worktree) by replacing the old parent prefix with the new one. The registry
entry is moved to the new bare path as well.`,
		Args: cobra.RangeArgs(0, 1),
		RunE: runLink,
	}
}

func runLink(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	bare, err := resolveLinkBare(cwd, args)
	if err != nil {
		return err
	}

	// Repair git's own worktree admin files so subsequent commands can work
	// from inside worktrees whose gitfiles point at the old location.
	_ = gitx.Worktree.Repair(bare)

	cfg, err := state.LoadRepo(bare)
	if err != nil {
		return err
	}
	if cfg.Trunk == "" {
		return fmt.Errorf("%s is not a gg-managed repo", bare)
	}

	// Always refresh origin from the bare's git config — picks up out-of-band
	// `git remote set-url` changes even when the repo didn't move.
	cfg.Origin = gitx.Remote.URL(bare, "origin")

	oldBare := cfg.BareRepo
	var rewritten int
	moved := oldBare != "" && oldBare != bare
	if moved {
		oldPrefix := filepath.Dir(oldBare)
		newPrefix := filepath.Dir(bare)
		rewrite := func(p string) string {
			if strings.HasPrefix(p, oldPrefix) {
				return newPrefix + p[len(oldPrefix):]
			}
			return p
		}

		cfg.BareRepo = bare
		cfg.PrimaryWorktree = rewrite(cfg.PrimaryWorktree)

		branches, err := state.AllBranches(bare)
		if err != nil {
			return err
		}
		for _, b := range branches {
			newPath := rewrite(b.Worktree)
			if newPath != b.Worktree {
				if err := state.UpdateWorktree(bare, b.Name, newPath); err != nil {
					return err
				}
				rewritten++
			}
		}
	}

	if err := state.SaveRepo(bare, cfg); err != nil {
		return err
	}
	if moved {
		_ = registry.Remove(oldBare)
	}

	// Name derivation respects layout: for nested, basename of bare is ".bare"
	// and the repo name is its parent dir's basename.
	if err := registry.Upsert(registry.Entry{
		Name:            cfg.ShortName(),
		Bare:            bare,
		PrimaryWorktree: cfg.PrimaryWorktree,
		Trunk:           cfg.Trunk,
		Origin:          cfg.Origin,
	}); err != nil {
		return err
	}

	if rewritten > 0 {
		successf(
			"linked %s → %s  (%d branch worktree path%s rewritten)",
			cfg.ShortName(),
			bare,
			rewritten,
			plural(rewritten, "", "s"),
		)
	} else {
		successf("linked %s → %s", cfg.ShortName(), bare)
	}
	return nil
}

// resolveLinkBare figures out the new bare location. With an explicit arg, use
// it. Otherwise try git (works when the gitfile at cwd is still valid) and
// then fall back to scanning sibling dirs for a bare whose gg config points
// at it — this is the post-move recovery path where git can't resolve cwd
// because its `.git` gitfile is stale.
func resolveLinkBare(cwd string, args []string) (string, error) {
	if len(args) == 1 {
		bare, err := filepath.Abs(args[0])
		if err != nil {
			return "", err
		}
		if !isBareWithGotConfig(bare) {
			return "", fmt.Errorf("%s is not a gg-managed bare repo", bare)
		}
		return bare, nil
	}
	if bare, err := gitx.Revision.CommonDir(cwd); err == nil {
		if !filepath.IsAbs(bare) {
			if top, err := gitx.Revision.TopLevel(cwd); err == nil {
				bare = filepath.Join(top, bare)
			}
		}
		bare = filepath.Clean(bare)
		if isBareWithGotConfig(bare) {
			return bare, nil
		}
	}
	if bare, err := discoverBare(cwd); err == nil {
		return bare, nil
	}
	return "", fmt.Errorf("couldn't resolve bare repo — pass its path as an argument to `gg link`")
}

// discoverBare scans cwd's parent for a gg-managed bare repo. Used when
// git can't resolve cwd because its gitfile is stale.
//
// Both layouts are checked:
//   - nested: <container>/.bare sits alongside cwd (also a dir under container)
//   - flat:   <parent>/<basename>.git sits alongside cwd (a worktree dir)
func discoverBare(cwd string) (string, error) {
	parent := filepath.Dir(cwd)

	// Nested layout first — the common case for new repos.
	if nested := filepath.Join(parent, ".bare"); isBareWithGotConfig(nested) {
		return nested, nil
	}

	// Flat layout: conventional sibling by name.
	base := filepath.Base(cwd)
	if sibling := filepath.Join(parent, base+".git"); isBareWithGotConfig(sibling) {
		return sibling, nil
	}

	// Fallback: scan parent for any candidate (`.bare` or any `*.git` dir).
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() != ".bare" && !strings.HasSuffix(entry.Name(), ".git") {
			continue
		}
		p := filepath.Join(parent, entry.Name())
		if isBareWithGotConfig(p) {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no gg-managed bare repo found near %s", cwd)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf(
			"multiple bare repos near %s — pass the one you want to `gg link`",
			cwd,
		)
	}
}

func isBareWithGotConfig(dir string) bool {
	if !gitx.Revision.IsBareRepo(dir) {
		return false
	}
	cfg, err := state.LoadRepo(dir)
	if err != nil {
		return false
	}
	return cfg.Trunk != ""
}
