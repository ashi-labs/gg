package cli

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/registry"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/spf13/cobra"
)

const cloneLongHelp = `
Clone a repo from a remote repository into a gg managed repo and cd into the primary worktree.`

func newCloneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clone <url> [name]",
		Short: "Clone a repo.",
		Long:  cloneLongHelp,
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runClone,
	}
	return cmd
}

func runClone(cmd *cobra.Command, args []string) error {
	remoteURL := args[0]
	name := ""
	if len(args) == 2 {
		name = args[1]
	} else {
		name = repoNameFromURL(remoteURL)
	}
	if name == "" {
		return fmt.Errorf("could not derive repo name from %q; pass it as second arg", remoteURL)
	}
	return clone(remoteURL, name)
}

func clone(remoteURL, name string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	container := filepath.Join(cwd, name)
	bareDir := filepath.Join(container, ".bare")
	if _, err := os.Stat(container); err == nil {
		return fmt.Errorf("%s already exists", container)
	}
	if err := os.MkdirAll(container, 0o755); err != nil {
		return err
	}
	hintf("cloning bare: %s", bareDir)
	if err := gitx.Clone.Bare(container, remoteURL, bareDir); err != nil {
		return err
	}
	if err := gitx.Config.Set(
		bareDir,
		"remote.origin.fetch",
		"+refs/heads/*:refs/remotes/origin/*",
	); err != nil {
		return err
	}
	if err := gitx.Remote.FetchOrigin(bareDir); err != nil {
		return err
	}
	if err := gitx.Remote.SetHeadAuto(bareDir, "origin"); err != nil {
		return err
	}
	trunk, err := gitx.Ref.DefaultBranch(bareDir)
	if err != nil {
		return fmt.Errorf("could not resolve default branch: %w", err)
	}
	primary := filepath.Join(container, trunk)
	hintf("adding primary worktree on %s: %s", styleBranch(trunk), primary)
	if err := gitx.Worktree.Add(bareDir, primary, trunk); err != nil {
		return err
	}
	cfg := state.Repo{
		Origin:          remoteURL,
		Trunk:           trunk,
		BareRepo:        bareDir,
		PrimaryWorktree: primary,
		SyncStrategy:    "rebase",
		MergeStrategy:   "squash",
	}
	if err := state.SaveRepo(bareDir, cfg); err != nil {
		return err
	}
	if err := registry.Upsert(registry.Entry{
		Name: name, Bare: bareDir, PrimaryWorktree: primary, Trunk: trunk, Origin: remoteURL,
	}); err != nil {
		hintf("warning: could not update registry: %v", err)
	}
	successf("cloned %s (trunk: %s)", name, styleBranch(trunk))
	stdout(primary)
	return nil
}

func repoNameFromURL(raw string) string {
	s := raw
	if u, err := url.Parse(s); err == nil && u.Path != "" && strings.Contains(s, "://") {
		s = u.Path
	} else if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s, "://") {
		s = s[i+1:]
	}
	return strings.TrimSuffix(path.Base(s), ".git")
}
