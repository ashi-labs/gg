package gitx

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestSeed_CopiesMissingPaths(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	mustMkdir(t, filepath.Join(src, "node_modules", "lib"))
	mustWrite(t, filepath.Join(src, "node_modules", "lib", "index.js"), "export {}\n")
	mustWrite(t, filepath.Join(src, ".env"), "DB_URL=localhost\n")

	res := Worktree.Seed(src, dst, []string{"node_modules", ".env", "missing"})

	assertStringSet(t, res.Seeded, []string{"node_modules", ".env"})
	if len(res.Skipped) != 0 {
		t.Fatalf("unexpected skipped: %v", res.Skipped)
	}
	if got := mustRead(t, filepath.Join(dst, ".env")); got != "DB_URL=localhost\n" {
		t.Fatalf(".env not seeded: %q", got)
	}
	if got := mustRead(
		t,
		filepath.Join(dst, "node_modules", "lib", "index.js"),
	); got != "export {}\n" {
		t.Fatalf("node_modules not seeded: %q", got)
	}
}

func TestSeed_SkipsExistingDestination(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	mustWrite(t, filepath.Join(src, ".env"), "SRC\n")
	mustWrite(t, filepath.Join(dst, ".env"), "DST\n")

	res := Worktree.Seed(src, dst, []string{".env"})
	if len(res.Seeded) != 0 {
		t.Fatalf("expected no seeded, got %v", res.Seeded)
	}
	if got := mustRead(t, filepath.Join(dst, ".env")); got != "DST\n" {
		t.Fatalf("destination was clobbered: %q", got)
	}
}

func TestSeed_SelfSeedingIsNoOp(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".env"), "x\n")

	res := Worktree.Seed(dir, dir, []string{".env"})
	if len(res.Seeded) != 0 || len(res.Skipped) != 0 {
		t.Fatalf("self-seeding should be inert: %+v", res)
	}
}

func TestSeed_EmptyInputs(t *testing.T) {
	cases := []struct{ name, src, dst string }{
		{"empty-src", "", t.TempDir()},
		{"empty-dst", t.TempDir(), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Worktree.Seed(tc.src, tc.dst, []string{"x"})
			if len(res.Seeded)+len(res.Skipped) != 0 {
				t.Fatalf("expected no-op, got %+v", res)
			}
		})
	}
}

func TestSeed_RejectsUnsafeNames(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "secret"), "nope")

	// An absolute path and a parent-ref both resolve outside the
	// worktree root — Seed must refuse to follow them.
	res := Worktree.Seed(
		src,
		dst,
		[]string{filepath.Join(outside, "secret"), "../../etc/passwd", ".", ""},
	)
	if len(res.Seeded) != 0 {
		t.Fatalf("unsafe names leaked through: %v", res.Seeded)
	}
	if _, err := os.Stat(filepath.Join(dst, "secret")); err == nil {
		t.Fatalf("absolute-path entry was seeded")
	}
}

func TestSeed_PreservesSymlinks(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	pkg := filepath.Join(src, "node_modules", "pkg")
	mustMkdir(t, pkg)
	mustWrite(t, filepath.Join(pkg, "real.js"), "1\n")
	if err := os.Symlink("real.js", filepath.Join(pkg, "link.js")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	res := Worktree.Seed(src, dst, []string{"node_modules"})
	if len(res.Seeded) != 1 {
		t.Fatalf("expected one seeded entry, got %+v", res)
	}
	info, err := os.Lstat(filepath.Join(dst, "node_modules", "pkg", "link.js"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was not preserved as a symlink")
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func assertStringSet(t *testing.T, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("string sets differ:\n got:  %v\n want: %v", g, w)
	}
}
