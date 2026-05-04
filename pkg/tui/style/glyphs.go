package style

import (
	"fmt"
	"os"

	"github.com/ashi-labs/gg/pkg/config"
)

type GlyphSet struct {
	Name          string
	Repo          string
	Trunk         string
	Branch        string
	CurrentBranch string
	Caret         string
	Success       string
	Failure       string
	Pending       string
	Skipped       string
	Dirty         string
	Hint          string
	PRGlyphs      PRGlyphSet
}

// Glyphs related to PR status
type PRGlyphSet struct {
	Open          string
	Draft         string
	Merged        string
	Closed        string
	ChecksPassed  string
	ChecksFailed  string
	ChecksPending string
}

var unicode = GlyphSet{
	Name:          config.GlyphsUnicode,
	Trunk:         "●",
	Branch:        "○",
	CurrentBranch: "●",
	Repo:          "●",
	Caret:         "◂",
	Pending:       "○",
	Success:       "✓",
	Failure:       "✗",
	Skipped:       "·",
	Dirty:         "●",
	Hint:          "→",
	PRGlyphs: PRGlyphSet{
		Open:          "○",
		Draft:         "◌",
		Merged:        "●",
		Closed:        "⊘",
		ChecksPassed:  "✓",
		ChecksFailed:  "✗",
		ChecksPending: "⧗", // hourglass
	},
}

var nerd = GlyphSet{
	Name:          config.GlyphsNerd,
	Trunk:         "", // nf-dev-git_branch
	Branch:        "○",
	CurrentBranch: "●",
	Repo:          "", // nf-fa-github — the classic octocat silhouette
	Caret:         "", // nf-fa-chevron_left
	Pending:       "", // nf-fa-circle_o (empty)
	Success:       "", // nf-fa-check
	Failure:       "", // nf-fa-times
	Skipped:       "", // nf-fa-minus
	Dirty:         "", // nf-fa-warning
	Hint:          "", // nf-fa-lightbulb
	PRGlyphs: PRGlyphSet{
		Open:          "", // nf-oct-file_diff
		Draft:         "", // nf-oct-pencil
		Merged:        "", // nf-oct-git_merge
		Closed:        "", // nf-oct-no_entry
		ChecksPassed:  "", // nf-fa-check
		ChecksFailed:  "", // nf-fa-times
		ChecksPending: "", // nf-fa-hourglass_half
	},
}

// GlyphSetNames returns the registered glyph-set names sorted for
// help text / future `gg config` listing.
func GlyphSetNames() []string {
	return []string{
		config.GlyphsUnicode,
		config.GlyphsNerd,
	}
}

// Glyphs is the resolved glyph set for this process. Populated once at
// package init from the user config (or the GG_GLYPHS env override,
// matching GG_THEME's shape — same opt-out-by-env pattern).
var Glyphs = resolveGlyphs()

func resolveGlyphs() GlyphSet {
	name := os.Getenv("GG_GLYPHS")
	if name == "" {
		name = config.Load().Glyphs
	}
	switch name {
	case config.GlyphsUnicode:
		return unicode
	case config.GlyphsNerd:
		return nerd
	case "":
		return unicode
	default:
		fmt.Fprintf(os.Stderr,
			"gg: unknown glyphs %q, falling back to %q (valid: %v)\n",
			name, config.DefaultGlyphs, GlyphSetNames())
		return unicode
	}
}
