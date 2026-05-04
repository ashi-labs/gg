package cli

import (
	"path/filepath"
	"strings"

	"github.com/ashi-labs/gg/pkg/gitx"
)

// pathSuggester translates worktree-root-relative paths (the form `git
// status --porcelain` and most git plumbing emit) into completion
// suggestions shaped like the user is actually typing — with `./`,
// `../`, plain, or subdir-relative all handled uniformly through
// stdlib filepath ops rather than hand-rolled prefix detection.
//
// One pass:
//
//  1. filepath.Split(toComplete) → typedDir + namePrefix.
//  2. Resolve typedDir to absolute against cwd via filepath.Abs.
//  3. For each candidate (worktree-rel), resolve to absolute then take
//     filepath.Rel(absTypedDir, ...). Anything that escapes absTypedDir
//     is filtered (different subtree).
//  4. Reattach the user's verbatim typedDir to the result so the shell's
//     prefix-match against `toComplete` accepts it.
//
// Designed to be reused across `add`, `rm`, `restore`, `diff <path>`,
// etc. Cheap: one `git rev-parse --show-toplevel` per construction.
type pathSuggester struct {
	worktreeTop string
	typedDir    string // user's dir prefix, verbatim ("./", "../", "pkg/", "")
	namePrefix  string // partial name to match against candidate
	absTypedDir string // typedDir resolved to absolute against cwd
}

// newPathSuggester resolves the worktree top and pre-splits toComplete
// for reuse across many suggest() calls in one completion pass.
// Returns an error if cwd isn't inside a git worktree — completion
// callers should fall back to ShellCompDirectiveDefault in that case.
func newPathSuggester(cwd, toComplete string) (*pathSuggester, error) {
	top, err := gitx.Revision.TopLevel(cwd)
	if err != nil {
		return nil, err
	}
	typedDir, namePrefix := filepath.Split(toComplete)
	absTypedDir, err := filepath.Abs(filepath.Join(cwd, typedDir))
	if err != nil {
		return nil, err
	}
	return &pathSuggester{
		worktreeTop: top,
		typedDir:    typedDir,
		namePrefix:  namePrefix,
		absTypedDir: absTypedDir,
	}, nil
}

// suggest returns the completion suggestion for a worktree-root-relative
// candidate, or "" if it doesn't belong (different subtree from the
// user's typed dir, or basename doesn't share the typed prefix).
func (p *pathSuggester) suggest(worktreeRelPath string) string {
	abs := filepath.Join(p.worktreeTop, worktreeRelPath)
	rel, err := filepath.Rel(p.absTypedDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	if !strings.HasPrefix(rel, p.namePrefix) {
		return ""
	}
	return p.typedDir + rel
}
