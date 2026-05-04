package cli

import (
	"fmt"

	"github.com/ashi-labs/gg/pkg/registry"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

func newCleanupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cleanup",
		Aliases: []string{"clean"},
		Short:   "prune registry entries whose bare repo is missing on disk.",
		Args:    cobra.NoArgs,
		RunE:    runCleanup,
	}
	cmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func runCleanup(cmd *cobra.Command, args []string) error {
	entries, err := registry.Load()
	if err != nil {
		return err
	}
	var invalid []registry.Entry
	for _, e := range entries {
		if e.Validate() != registry.StatusOK {
			invalid = append(invalid, e)
		}
	}
	if len(invalid) == 0 {
		successf("registry is clean — nothing to remove")
		return nil
	}

	renderCleanupListing(invalid)

	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		confirmed, err := confirmCleanup(len(invalid))
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("cancelled")
		}
	}

	for _, e := range invalid {
		if err := registry.Remove(e.Bare); err != nil {
			return err
		}
	}
	successf("removed %d stale entr%s", len(invalid), plural(len(invalid), "y", "ies"))
	return nil
}

// plural returns singular when n == 1, pluralForm otherwise. Shared
// with a few other commands (delete, link) that emit counts.
func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}

// renderCleanupListing shows the invalid entries in a borderless
// table — name in Branch, reason badged in Error, bare path dimmed.
// Replaces the old three-space-separated line format which was hard
// to scan when entries shared a reason prefix.
func renderCleanupListing(invalid []registry.Entry) {
	hintf("%d stale entr%s to remove:", len(invalid), plural(len(invalid), "y", "ies"))
	plainln("")
	t := table.New().
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).BorderBottom(false).
		BorderLeft(false).BorderRight(false)
	for _, e := range invalid {
		t.Row(
			style.Stderr.Branch.Render(e.Name),
			style.Stderr.Error.Render("("+e.Validate().String()+")"),
			style.Stderr.Dim.Render(e.Bare),
		)
	}
	plainln(t.String())
	plainln("")
}

// confirmCleanup defers to the shared confirmYesNo helper. Kept as a thin
// wrapper so the call site reads naturally in runCleanup.
func confirmCleanup(n int) (bool, error) {
	return confirmYesNo(fmt.Sprintf("remove %d stale %s?", n, plural(n, "entry", "entries")))
}

// huhTheme starts from huh's ThemeBase and overrides the key color
// surfaces to match our palette — title in Hint, focused button in
// Dirty (the "about to commit" accent we also use on the picker
// caret), blurred button muted. Anything we don't touch keeps base
// defaults, which works fine in both light and dark terminals.
//
// Styles must be anchored to style.StderrRenderer rather than the default
// lipgloss renderer. Several huh-using commands (sync, cleanup) run under
// the shell wrapper which captures stdout — the default renderer probes
// stdout, sees a pipe, downgrades to Ascii, and silently drops the
// Background() that highlights the focused button. Stderr is the TTY
// huh actually paints to, so anchoring there keeps the highlight visible.
func huhTheme() *huh.Theme {
	r := style.StderrRenderer
	t := huh.ThemeBase()
	t.Focused.Title = style.Stderr.Hint
	t.Focused.Description = style.Stderr.Dim
	// Reverse the foreground/background relationship for max contrast on
	// the focused button: dark text (the theme's Background) on Dirty bg.
	// Body Foreground is a near-white tone in dark themes, which reads
	// poorly against orange — flipping to Background gives the unambiguous
	// "selected" look the user expects.
	t.Focused.FocusedButton = r.NewStyle().
		Padding(0, 2).
		MarginRight(1).
		Foreground(style.Current().Background).
		Background(style.Stderr.Dirty.GetForeground())
	t.Focused.BlurredButton = r.NewStyle().
		Padding(0, 2).
		MarginRight(1).
		Foreground(style.Stderr.Dim.GetForeground())
	t.Focused.Base = r.NewStyle()
	t.Blurred = t.Focused
	t.Help.Ellipsis = style.Stderr.Dim
	t.Help.FullDesc = style.Stderr.Dim
	t.Help.FullKey = style.Stderr.Dim
	t.Help.FullSeparator = style.Stderr.Dim
	t.Help.ShortDesc = style.Stderr.Dim
	t.Help.ShortKey = style.Stderr.Dim
	t.Help.ShortSeparator = style.Stderr.Dim
	return t
}
