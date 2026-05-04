package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/gitx/forge"
	"github.com/ashi-labs/gg/pkg/registry"
	"github.com/ashi-labs/gg/pkg/state"
)

func resolveCtx() (string, string, *state.Repo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", nil, err
	}
	bare, err := gitx.Revision.CommonDir(cwd)
	if err != nil {
		return "", "", nil, fmt.Errorf("not inside a git repository")
	}
	if !filepath.IsAbs(bare) {
		top, err := gitx.Revision.TopLevel(cwd)
		if err != nil {
			return "", "", nil, err
		}
		bare = filepath.Join(top, bare)
	}
	bare = filepath.Clean(bare)
	repo, err := state.LoadRepo(bare)
	if err != nil {
		return "", "", nil, err
	}
	if repo.Trunk == "" {
		// Two distinct setup paths cover every case:
		//   * `gg init <url>` for a fresh checkout (clones into a new
		//     bare and writes the trunk key).
		//   * `gg link` for a bare that was created another way (manual
		//     clone, moved from another machine) — registers it with
		//     gg and writes the trunk key into its config.
		return "", "", nil, fmt.Errorf(
			"gg is missing a `gg.trunk` entry for this repo — run `gg init <url>` in a fresh directory, or `gg link` from inside a bare repo to start tracking with gg",
		)
	}
	// picks up out-of-band `git remote set-url` changes so forge selection always matches the
	// live remote.
	if liveOrigin := gitx.Remote.URL(
		bare,
		"origin",
	); liveOrigin != "" &&
		liveOrigin != repo.Origin {
		repo.Origin = liveOrigin
		_ = state.SaveRepo(bare, repo)
	}
	// best effort: bump LastUsedAt so `gg repos` can sort by recency
	_ = registry.Touch(bare)
	gitx.Forge = forge.Select(repo.Origin)
	return cwd, bare, &repo, nil
}
