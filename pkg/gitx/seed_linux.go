package gitx

import (
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// clonePath reproduces src at dst using ioctl(FICLONE) for regular
// files. Linux has no tree-level clone syscall, so we walk the source
// once and reflink each file: O(files) syscalls, but each is an O(1)
// metadata op with zero bytes copied. Symlinks are recreated as
// symlinks. If any FICLONE fails (ext4 without reflink, cross-fs, ...)
// we abort — Seed's caller removes the partial tree and reports the
// path as skipped.
func clonePath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if !info.IsDir() {
		return reflinkFile(src, dst, info.Mode().Perm())
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			di, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, di.Mode().Perm())
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			di, err := d.Info()
			if err != nil {
				return err
			}
			return reflinkFile(path, target, di.Mode().Perm())
		}
	})
}

func reflinkFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if err := unix.IoctlFileClone(int(out.Fd()), int(in.Fd())); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}
