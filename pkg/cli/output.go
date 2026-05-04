package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/ashi-labs/gg/pkg/tui/style"
)

type output struct {
	stdout  io.Writer
	stderr  io.Writer
	palette style.Palette
	glyphs  style.GlyphSet
}

var out = &output{
	stdout:  os.Stdout,
	stderr:  os.Stderr,
	palette: style.Stderr,
	glyphs:  style.Glyphs,
}

func successf(format string, args ...any) {
	_, _ = fmt.Fprintf(out.stderr, "%s %s\n",
		out.palette.Success.Render(out.glyphs.Success),
		fmt.Sprintf(format, args...))
}

func ssuccessf(format string, args ...any) string {
	return fmt.Sprintf("%s %s\n",
		out.palette.Success.Render(out.glyphs.Success),
		fmt.Sprintf(format, args...))
}

func errorf(format string, args ...any) {
	_, _ = fmt.Fprintf(out.stderr, "%s %s\n",
		out.palette.Error.Render(out.glyphs.Failure),
		fmt.Sprintf(format, args...))
}

func serrorf(format string, args ...any) string {
	return fmt.Sprintf("%s %s\n",
		out.palette.Error.Render(out.glyphs.Failure),
		fmt.Sprintf(format, args...))
}

func hintf(format string, args ...any) {
	_, _ = fmt.Fprintf(out.stderr, "%s %s\n",
		out.palette.Hint.Render(out.glyphs.Hint),
		fmt.Sprintf(format, args...))
}

func shintf(format string, args ...any) string {
	return fmt.Sprintf("%s %s\n",
		out.palette.Hint.Render(out.glyphs.Hint),
		fmt.Sprintf(format, args...))
}

func plainln(s string) { _, _ = fmt.Fprintln(out.stderr, s) }

// styleBranch renders a branch name with the branch palette so output
// messages reference branches consistently.
func styleBranch(name string) string { return out.palette.Branch.Render(name) }

// writes a path to stdout. consumed by the shell wrapper's `cd`/`eval`.
func stdout(p string) { _, _ = fmt.Fprintln(out.stdout, p) }
