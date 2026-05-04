// Package style owns all visual styling for gg. It exposes two palettes —
// one bound to a stderr-probing renderer, one to a stdout-probing renderer —
// so color emission is decided per output stream rather than globally.
//
// Why two palettes? The shell wrapper captures stdout with $(command got ...)
// for commands like `gg append`. That makes stdout a pipe (not a TTY). If
// color detection probed stdout, lipgloss would disable ANSI across the
// board — including for status/hint messages that actually reach the user
// via stderr. With per-stream palettes:
//
//   - output.* messages (stderr) always render in color when stderr is a TTY,
//     regardless of whether stdout is captured.
//   - `gg log` (stdout) correctly strips color when piped into grep/less.
//   - Pickers (on /dev/tty) use the stderr palette since the two stream
//     probes agree in practice.
//
// Overrides, in precedence order from low to high:
//   - auto probing (the defaults)
//   - NO_COLOR env (handled automatically by termenv; always wins over probing)
//   - FORCE_COLOR / CLICOLOR_FORCE env (applied in init — bumps both
//     renderers to ANSI256 even without a TTY)
//   - --color=always|never|auto (applied in the root command's PreRun;
//     wins over the env because it's an explicit user request)
//
// Theming: the palette values come from a named Theme. Resolution precedence:
//
//  1. GG_THEME env var (for quick experiments / per-invocation override)
//  2. user config (`theme` key in ~/.config/gg/config.toml)
//  3. DefaultTheme ("tokyo-night")
//
// Unknown theme names fall back to DefaultTheme with a one-line stderr
// warning so typos don't silently paint everything the wrong color.
package style

import (
	"fmt"
	"os"
	"sort"

	"github.com/ashi-labs/gg/pkg/config"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// StderrRenderer probes os.Stderr. Use the Stderr palette for anything
// written to stderr (output package, picker labels).
var StderrRenderer = lipgloss.NewRenderer(os.Stderr)

// StdoutRenderer probes os.Stdout. Use the Stdout palette for anything
// written to stdout that users might reasonably pipe (e.g., `gg log`).
var StdoutRenderer = lipgloss.NewRenderer(os.Stdout)

func ForceColor() {
	StderrRenderer.SetColorProfile(termenv.ANSI256)
	StdoutRenderer.SetColorProfile(termenv.ANSI256)
}

func ForceNoColor() {
	StderrRenderer.SetColorProfile(termenv.Ascii)
	StdoutRenderer.SetColorProfile(termenv.Ascii)
}

type Palette struct {
	Success    lipgloss.Style
	Error      lipgloss.Style
	Hint       lipgloss.Style
	Repo       lipgloss.Style
	Trunk      lipgloss.Style
	Branch     lipgloss.Style
	Current    lipgloss.Style
	Dim        lipgloss.Style
	Dirty      lipgloss.Style
	Danger     lipgloss.Style
	Badge      lipgloss.Style
	Foreground lipgloss.Style
}

// Theme maps semantic color roles onto concrete terminal colors. Concrete
// themes are registered in `themes` below; `current` resolves one at init.
type Theme struct {
	Name       string
	Foreground lipgloss.TerminalColor
	Background lipgloss.TerminalColor
	Green      lipgloss.TerminalColor
	Red        lipgloss.TerminalColor
	Yellow     lipgloss.TerminalColor
	Orange     lipgloss.TerminalColor
	Blue       lipgloss.TerminalColor
	Magenta    lipgloss.TerminalColor
	Comment    lipgloss.TerminalColor
	Dim        lipgloss.TerminalColor
	BadgeFg    lipgloss.TerminalColor
	BadgeBg    lipgloss.TerminalColor
}

// Tokyo Night — the default. AdaptiveColor lets light-background terminals
// pick the Storm variant's darker-on-light hues while dark terminals get the
// canonical Night values. Values sourced from the official schemes.
var tokyoNight = Theme{
	Name: "tokyo-night",
	// Default body text. Dark uses the canonical Night foreground; Light
	// uses the Day variant's slate tone rather than the bluish `#3760bf`
	// so text reads as neutral body copy, not another accent.
	Foreground: lipgloss.AdaptiveColor{Light: "#343b58", Dark: "#c0caf5"},
	Background: lipgloss.AdaptiveColor{Light: "#e6e7ee", Dark: "222436"},
	Green:      lipgloss.AdaptiveColor{Light: "#485e30", Dark: "#9ece6a"},
	Red:        lipgloss.AdaptiveColor{Light: "#c64343", Dark: "#f7768e"},
	Yellow:     lipgloss.AdaptiveColor{Light: "#8c6c3e", Dark: "#e0af68"},
	Orange:     lipgloss.AdaptiveColor{Light: "#b15c00", Dark: "#ff9e64"},
	Blue:       lipgloss.AdaptiveColor{Light: "#34548a", Dark: "#7aa2f7"},
	Magenta:    lipgloss.AdaptiveColor{Light: "#5a3e8e", Dark: "#bb9af7"},
	Comment:    lipgloss.AdaptiveColor{Light: "#848cb5", Dark: "#565f89"},
	Dim:        lipgloss.AdaptiveColor{Light: "#9699a8", Dark: "#737aa2"},
	BadgeFg:    lipgloss.AdaptiveColor{Light: "#d5d6db", Dark: "#c0caf5"},
	BadgeBg:    lipgloss.AdaptiveColor{Light: "#b4b5bb", Dark: "#414868"},
}

// Inherit — use the terminal's own palette via ANSI color codes 0-15. The
// user's scheme decides what these look like; gg just picks semantic
// slots. No orange exists in 0-15, so Dirty borrows bright yellow (11);
// Comment/Dim reuse bright black (8).
var inherit = Theme{
	Name:       "inherit",
	Foreground: lipgloss.ANSIColor(15),
	Background: lipgloss.ANSIColor(0),
	Green:      lipgloss.ANSIColor(2),
	Red:        lipgloss.ANSIColor(1),
	Yellow:     lipgloss.ANSIColor(3),
	Orange:     lipgloss.ANSIColor(16),
	Blue:       lipgloss.ANSIColor(4),
	Magenta:    lipgloss.ANSIColor(5),
	Comment:    lipgloss.ANSIColor(8),
	Dim:        lipgloss.ANSIColor(8),
	BadgeFg:    lipgloss.ANSIColor(7),
	BadgeBg:    lipgloss.ANSIColor(8),
}

var themes = map[string]Theme{
	tokyoNight.Name: tokyoNight,
	inherit.Name:    inherit,
}

// ThemeNames returns the registered theme names, sorted. Useful for help text
// and for a future `gg config` command.
func ThemeNames() []string {
	names := make([]string, 0, len(themes))
	for n := range themes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// current is the theme chosen at init. paletteFor consults it when building
// the two palettes.
var current = resolveTheme()

// Current returns the theme that was resolved at package init. Exported for
// dev-only diagnostics (see `gg style debug` under -tags dev).
func Current() Theme { return current }

func resolveTheme() Theme {
	requested := os.Getenv("GG_THEME")
	if requested == "" {
		requested = config.Load().UI.Theme
	}
	if t, ok := themes[requested]; ok {
		return t
	}
	// Typo / removed theme — warn once, stay usable.
	fmt.Fprintf(os.Stderr,
		"gg: unknown theme %q, falling back to %q (available: %v)\n",
		requested, config.DefaultTheme, ThemeNames())
	return themes[config.DefaultTheme]
}

func paletteFor(r *lipgloss.Renderer) Palette {
	return Palette{
		Success: r.NewStyle().Foreground(current.Green),
		Error:   r.NewStyle().Foreground(current.Red),
		Hint:    r.NewStyle().Foreground(current.Magenta),
		Trunk:   r.NewStyle().Foreground(current.Yellow),
		Branch:  r.NewStyle().Foreground(current.Blue),
		Current: r.NewStyle().Foreground(current.Magenta),
		Dim:     r.NewStyle().Foreground(current.Dim),
		Dirty:   r.NewStyle().Foreground(current.Orange),
		Danger:  r.NewStyle().Foreground(current.Red),
		Badge: r.NewStyle().
			Foreground(current.BadgeFg).
			Background(current.BadgeBg).
			Padding(0, 1),
		Foreground: r.NewStyle().Foreground(current.Foreground),
		Repo:       r.NewStyle().Foreground(current.Green),
	}
}

// Stderr is the palette for stderr-bound text.
var Stderr = paletteFor(StderrRenderer)

// Stdout is the palette for stdout-bound text.
var Stdout = paletteFor(StdoutRenderer)
