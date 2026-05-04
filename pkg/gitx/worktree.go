package gitx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type xWorktree struct{}

func (xWorktree) Add(bareDir, path, branch string) error {
	return In(bareDir).Cmd(kWorktree, kAdd, path, branch).Err()
}

func (xWorktree) AddWithBranch(bareDir, path, branch, parent string) error {
	return In(bareDir).Cmd(kWorktree, kAdd, dashB, branch, path, parent).Err()
}

func (xWorktree) List(dir string) (string, error) {
	return In(dir).Cmd(kWorktree, kList, flagPorcelain).String()
}

func (xWorktree) Move(bareDir, oldPath, newPath string) error {
	return In(bareDir).Cmd(kWorktree, kMove, oldPath, newPath).Err()
}

func (xWorktree) Remove(bareDir, path string) error {
	return In(bareDir).Cmd(kWorktree, kRemove, flagForce, path).Err()
}

func (xWorktree) Repair(bareDir string) error {
	return In(bareDir).Cmd(kWorktree, kRepair).Err()
}

type SeedResult struct {
	Seeded  []string
	Skipped []string
}

// Seed copies the given top-level paths (files or directories) from src
// into dst using filesystem-level CoW cloning: clonefile(2) on macOS,
// ioctl(FICLONE) on Linux. On APFS a whole directory tree clones in
// one syscall; on btrfs/xfs each regular file clones via FICLONE in
// O(1) metadata with zero bytes copied. Missing sources are skipped
// silently; an existing destination is also skipped so Seed never
// clobbers state that `git worktree add` already laid down.
//
// When the filesystem doesn't support reflink (non-APFS macOS, ext4,
// cross-device seeding) the path is reported in Skipped. Seed
// intentionally does NOT fall through to a byte copy — silently
// duplicating gigabytes of gitignored state is the wrong surprise to
// ship.
//
// Seed never fails the outer command — callers get a per-path
// accounting in SeedResult and decide what (if anything) to log.
func (xWorktree) Seed(src, dst string, paths []string) SeedResult {
	var res SeedResult
	if src == "" || dst == "" || len(paths) == 0 {
		return res
	}
	if absEqual(src, dst) {
		return res
	}
	for _, name := range paths {
		if name == "" || name == "." || name == ".." {
			continue
		}
		// Reject anything that would escape the worktree root. Seed
		// keys are configured, not user-supplied per invocation, but a
		// stray "../foo" in config should not become a footgun.
		if filepath.IsAbs(name) || containsParentRef(name) {
			continue
		}
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
		if _, err := os.Lstat(srcPath); err != nil {
			continue
		}
		if _, err := os.Lstat(dstPath); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := clonePath(srcPath, dstPath); err != nil {
			res.Skipped = append(res.Skipped, name)
			// Clean up a partial tree from a mid-walk failure so the
			// next run can retry cleanly.
			_ = os.RemoveAll(dstPath)
			continue
		}
		res.Seeded = append(res.Seeded, name)
	}
	return res
}

func absEqual(a, b string) bool {
	aa, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

func containsParentRef(name string) bool {
	cleaned := filepath.Clean(name)
	if cleaned == ".." {
		return true
	}
	return strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, `..\`)
}
