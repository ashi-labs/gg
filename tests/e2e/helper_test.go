package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sharedExec is a hermetically-sandboxed local executor shared by every test.
// Per-test isolation comes from distinct workdirs (t.TempDir()) and a
// per-test XDG_STATE_HOME, not fresh sandboxes.
var sharedExec *localExec

// sharedUpstream is a seeded bare repo (one commit on main) created once in
// TestMain. Tests clone from it; no test pushes back, so it's safe to share.
var sharedUpstream string

// minGitMajor / minGitMinor pin the lowest git version known to support
// every primitive gg drives (worktree move, --initial-branch, etc.).
const (
	minGitMajor = 2
	minGitMinor = 30
)

func TestMain(m *testing.M) {
	if err := requireGit(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	tmp, err := os.MkdirTemp("", "gg-e2e-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)

	binPath := filepath.Join(tmp, "gg")
	build := exec.Command("go", "build", "-o", binPath, "../../cmd/gg")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("building gg: " + err.Error())
	}

	// Per-suite "system home" — git/gg config files for the sandbox land here.
	suiteHome := filepath.Join(tmp, "home")
	if err := os.MkdirAll(suiteHome, 0o755); err != nil {
		panic(err)
	}

	sharedExec = &localExec{
		binPath: binPath,
		baseEnv: sandboxEnv(suiteHome),
	}

	sharedUpstream = filepath.Join(tmp, "upstream.git")
	if err := seedSharedUpstream(sharedExec, sharedUpstream); err != nil {
		panic("seeding shared upstream: " + err.Error())
	}

	os.Exit(m.Run())
}

func requireGit() error {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return fmt.Errorf("e2e tests require `git` on PATH: %w", err)
	}
	// Output is "git version X.Y.Z(.platformextras)".
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 3 {
		return fmt.Errorf("unexpected `git --version` output: %q", out)
	}
	parts := strings.SplitN(fields[2], ".", 3)
	if len(parts) < 2 {
		return fmt.Errorf("unparseable git version: %q", fields[2])
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	if major < minGitMajor || (major == minGitMajor && minor < minGitMinor) {
		return fmt.Errorf(
			"e2e tests require git >= %d.%d (found %s)",
			minGitMajor, minGitMinor, fields[2],
		)
	}
	return nil
}

// sandboxEnv assembles the environment every git/gg invocation runs under.
// HOME and XDG dirs point inside the suite's tempdir so nothing the tests do
// can touch the developer's real config. GIT_CONFIG_{GLOBAL,SYSTEM} neuter
// any leftover repo-discovery paths. Author/committer identity is fixed so
// commits don't depend on the developer's gitconfig.
func sandboxEnv(home string) []string {
	return []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.invalid",
		"PATH=" + os.Getenv("PATH"),
	}
}

func seedSharedUpstream(c *localExec, upstream string) error {
	if err := os.MkdirAll(filepath.Dir(upstream), 0o755); err != nil {
		return err
	}
	ctx := context.Background()
	if _, _, err := c.exec(ctx, "", "git", "init", "--bare", "--initial-branch=main", upstream); err != nil {
		return err
	}
	seed, err := os.MkdirTemp("", "gg-e2e-seed-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(seed)
	if _, _, err := c.exec(ctx, "", "git", "clone", upstream, seed); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("hi\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"git", "-C", seed, "add", "README.md"},
		{"git", "-C", seed, "commit", "-m", "initial"},
		{"git", "-C", seed, "push", "origin", "main"},
	} {
		if _, _, err := c.exec(ctx, "", args...); err != nil {
			return fmt.Errorf("%v: %w", args, err)
		}
	}
	return nil
}

// localExec runs git/gg commands locally with a fixed sandbox env. Same
// surface as the previous testcontainers-based cntr so test bodies don't
// have to care which one is in use.
type localExec struct {
	binPath string   // absolute path to the gg binary
	baseEnv []string // sandbox env applied to every command (gg + git)
}

// exec runs the given command line. The first arg may be "gg" (in which case
// it's resolved to the test's gg binary) or any binary on PATH. workDir
// pins cmd.Dir; pass "" to use the caller's cwd.
func (l *localExec) exec(
	ctx context.Context,
	workDir string,
	args ...string,
) (stdout, stderr string, err error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("exec: no args")
	}
	prog := args[0]
	if prog == "gg" {
		prog = l.binPath
	}
	cmd := exec.CommandContext(ctx, prog, args[1:]...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = append([]string(nil), l.baseEnv...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	stdout = strings.TrimSpace(outBuf.String())
	stderr = errBuf.String()
	if runErr != nil {
		return stdout, stderr, fmt.Errorf("exit %v: %v\nstderr: %s", runErr, args, stderr)
	}
	return stdout, stderr, nil
}

// withState returns a copy of l whose env also sets XDG_STATE_HOME, so per-
// test gg invocations get an isolated registry.
func (l *localExec) withState(state string) *localExec {
	env := append([]string(nil), l.baseEnv...)
	env = append(env, "XDG_STATE_HOME="+state)
	return &localExec{binPath: l.binPath, baseEnv: env}
}

// env holds per-test scratch dirs and a state-scoped executor.
type env struct {
	t        *testing.T
	c        *localExec // per-test executor (sandbox env + this test's XDG_STATE_HOME)
	work     string     // dir in which `gg init`/`gg clone` run
	upstream string     // bare repo acting as origin
	state    string     // XDG_STATE_HOME for this test
}

// newEnv provisions a fresh tempdir for the test. Upstream is the shared
// seeded bare repo (same for every test); work is an empty subdir. t.TempDir
// auto-removes everything when the test ends.
//
// On macOS t.TempDir returns a path under /var/folders/... but the kernel
// silently resolves /var → /private/var, so anything that round-trips
// through `os.Getwd` or `git rev-parse --show-toplevel` (i.e., gg's stdout)
// comes back with the /private/var prefix. Resolve once here so test
// assertions and gg's reported paths agree.
func newEnv(t *testing.T) *env {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(base, "work")
	state := filepath.Join(base, "state")
	for _, d := range []string{work, state} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &env{
		t:        t,
		c:        sharedExec.withState(state),
		work:     work,
		upstream: sharedUpstream,
		state:    state,
	}
}

// dirUnder returns a fresh subdir path under this env's tempdir base.
func (e *env) dirUnder(name string) string {
	e.t.Helper()
	path := filepath.Join(filepath.Dir(e.work), name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		e.t.Fatal(err)
	}
	return path
}

// withOwnUpstream replaces env.upstream with a per-test-scoped bare repo
// (cloned from the shared seed). Use this in tests that push to origin.
func (e *env) withOwnUpstream() *env {
	e.t.Helper()
	own := filepath.Join(filepath.Dir(e.work), "upstream.git")
	mustExec(e.t, e.c, "", "git", "clone", "--bare", sharedUpstream, own)
	e.upstream = own
	return e
}

func (e *env) gg(dir string, args ...string) (string, error) {
	e.t.Helper()
	stdout, _, err := e.c.exec(context.Background(), dir, append([]string{"gg"}, args...)...)
	return stdout, err
}

func (e *env) ggMust(dir string, args ...string) string {
	e.t.Helper()
	out, err := e.gg(dir, args...)
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

// exists reports whether path exists.
func (e *env) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readFile returns the contents of path.
func (e *env) readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

// writeFile puts content at path. Parent dirs must already exist.
func (e *env) writeFile(path, content string) {
	e.t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		e.t.Fatalf("writeFile %s: %v", path, err)
	}
}

// commitInto stages + commits a single file in a worktree dir.
func (e *env) commitInto(dir, name, content, msg string) {
	e.t.Helper()
	e.writeFile(filepath.Join(dir, name), content)
	mustExec(e.t, e.c, dir, "git", "add", name)
	mustExec(e.t, e.c, dir, "git", "commit", "-m", msg)
}

func mustExec(t *testing.T, c *localExec, dir string, args ...string) string {
	t.Helper()
	out, _, err := c.exec(context.Background(), dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
