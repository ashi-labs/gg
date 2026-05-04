package gitx

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Builder is the generic git command builder pattern
type builder struct{ dir string }

// cretaes a builder rooted at dir, "" roots at cwd
func In(dir string) builder { return builder{dir: dir} }

// starts building a command
func (b builder) Cmd(args ...string) *Cmd {
	return &Cmd{dir: b.dir, args: append([]string(nil), args...)}
}

type Cmd struct {
	dir   string
	args  []string
	stdin io.Reader
	env   []string
	pipe  bool // whether to inherit os.Stdin/Stdout/Stderr
}

func (c *Cmd) Args(args ...string) *Cmd {
	c.args = append(c.args, args...)
	return c
}

func (c *Cmd) Stdin(s string) *Cmd {
	c.stdin = strings.NewReader(s)
	return c
}

func (c *Cmd) StdinReader(r io.Reader) *Cmd {
	c.stdin = r
	return c
}

// appends KEY=VALUE pairs to the command's environment
func (c *Cmd) Env(kv ...string) *Cmd {
	c.env = append(c.env, kv...)
	return c
}

// wires stdin/stdout/stderr through the parent process. useful for interactive ops (rebase --interactive)
func (c *Cmd) Pipe() *Cmd {
	c.pipe = true
	return c
}

func (c *Cmd) build() *exec.Cmd {
	x := exec.Command(kGit, c.args...)
	if c.dir != "" {
		x.Dir = c.dir
	}
	if len(c.env) > 0 {
		x.Env = append(os.Environ(), c.env...)
	}
	return x
}

// same output and error output as Bytes() but as a trimmed string.
func (c *Cmd) String() (string, error) {
	out, err := c.Bytes()
	return strings.TrimSpace(string(out)), err
}

// raw bytes. folds stderr outpur into the error msg
func (c *Cmd) Bytes() ([]byte, error) {
	x := c.build()
	if c.stdin != nil {
		x.Stdin = c.stdin
	}
	var out, errOut bytes.Buffer
	x.Stdout = &out
	x.Stderr = &errOut
	if err := x.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(c.args, " "), msg)
	}
	return out.Bytes(), nil
}

// splits output into trimmed lines
func (c *Cmd) Lines() ([]string, error) {
	s, err := c.String()
	if err != nil || s == "" {
		return nil, err
	}
	return strings.Split(s, "\n"), nil
}

// returns only the stderr output of Bytes(). useful when output of stdout is not important
func (c *Cmd) Err() error {
	_, err := c.Bytes()
	return err
}

// (0, nil) on no err, (1, nil) on expected err, (-1, err) on unexpected err
func (c *Cmd) ExitCode() (int, error) {
	x := c.build()
	if c.stdin != nil {
		x.Stdin = c.stdin
	}
	if err := x.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return -1, fmt.Errorf("git %s: %w", strings.Join(c.args, " "), err)
	}
	return 0, nil
}

func (c *Cmd) Run() error {
	x := c.build()
	if c.stdin != nil || c.pipe {
		if c.stdin != nil {
			x.Stdin = c.stdin
		} else {
			x.Stdin = os.Stdin
		}
	}
	x.Stdout = os.Stdout
	x.Stderr = os.Stderr
	return x.Run()
}
