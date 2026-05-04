// go:build debug
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/ashi-labs/gg/pkg/config"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/spf13/cobra"
)

// To include debug commands in a local build:
//
//	go build -tags debug ./cmd
func init() {
	debugCommands = append(debugCommands, newStyleCmd())
}

func newStyleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "(dev) debug gg internals",
		Long:  "Diagnostic helpers for gg. Only compiled under -tags debug.",
	}
	cmd.AddCommand(newThemeCmd())
	cmd.AddCommand(newGlyphsCmd())
	return cmd
}

func newThemeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "theme",
		Short: "(dev) print theme resolution inputs and a palette sample",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			resolved := style.Current()
			lines := []string{
				"---- theme resolution ----",
				fmt.Sprintf("GG_THEME env       = %q", os.Getenv("GG_THEME")),
				fmt.Sprintf("config path        = %s", cfg.Path),
				fmt.Sprintf("config present     = %v", cfg.Path != ""),
				fmt.Sprintf("config theme       = %q", cfg.UI.Theme),
				fmt.Sprintf("available themes   = %s", strings.Join(style.ThemeNames(), ", ")),
				fmt.Sprintf("default theme      = %q", config.DefaultTheme),
				fmt.Sprintf("resolved theme     = %q", resolved.Name),
				"",
				"---- stderr palette sample ----",
			}
			plainln(strings.Join(lines, "\n"))
			successf("success")
			errorf("error")
			hintf("hint")
			plainln(style.Stderr.Trunk.Render("● trunk"))
			plainln(style.Stderr.Branch.Render("● branch"))
			plainln(style.Stderr.Current.Render("● current"))
			plainln(style.Stderr.Dirty.Render("● dirty"))
			plainln(style.Stderr.Dim.Render("● dim"))
			plainln(style.Stderr.Badge.Render("badge"))
			return nil
		},
	}
}

func newGlyphsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "glyphs",
		Short: "(dev) print glyph resolution and the active glyph set alongside unicode/nerd for comparison",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			resolved := style.Glyphs
			// Keep the order stable so eyeballing which symbol moved
			// between renders is trivial. Mirrors GlyphSet's field
			// order in glyphs.go.
			roles := []struct {
				name, active string
			}{
				{"Trunk", resolved.Trunk},
				{"Branch", resolved.Branch},
				{"Current", resolved.CurrentBranch},
				{"Repo", resolved.Repo},
				{"Caret", resolved.Caret},
				{"SyncPending", resolved.Pending},
				{"SyncSuccess", resolved.Success},
				{"SyncFailed", resolved.Failure},
				{"SyncSkipped", resolved.Skipped},
				{"Dirty", resolved.Dirty},
			}
			head := []string{
				"---- glyph resolution ----",
				fmt.Sprintf("GG_GLYPHS env      = %q", os.Getenv("GG_GLYPHS")),
				fmt.Sprintf("config path        = %s", cfg.Path),
				fmt.Sprintf("config present     = %v", cfg.Path != ""),
				fmt.Sprintf("config glyphs      = %q", cfg.UI.Glyphs),
				fmt.Sprintf("available sets     = %s", strings.Join(style.GlyphSetNames(), ", ")),
				fmt.Sprintf("default set        = %q", config.DefaultGlyphs),
				fmt.Sprintf("resolved set       = %q", resolved.Name),
				"",
				"---- active glyphs ----",
			}
			for _, r := range roles {
				head = append(head, fmt.Sprintf("  %-14s %s", r.name, r.active))
			}
			plainln(strings.Join(head, "\n"))
			return nil
		},
	}
}
