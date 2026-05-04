package gitx

import "golang.org/x/sys/unix"

// clonePath clones src to dst via the Darwin clonefile(2) syscall.
// For directories this is a single syscall that replicates the entire
// tree with CoW-shared extents — no per-file walk, no bytes copied.
// CLONE_NOFOLLOW preserves symlinks as symlinks rather than following
// them into whatever they point at.
func clonePath(src, dst string) error {
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
}
