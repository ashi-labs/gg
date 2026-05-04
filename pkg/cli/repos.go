package cli

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ashi-labs/gg/pkg/config"
	"github.com/ashi-labs/gg/pkg/registry"
	"github.com/ashi-labs/gg/pkg/tui/picker"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

func newReposCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repos",
		Short: "pick a tracked repo and cd to its primary worktree.",
		Args:  cobra.NoArgs,
		RunE:  runRepos,
	}
	cmd.Flags().
		Bool("list", false, "print the registry as plain text instead of launching the picker")
	cmd.Flags().
		String("sort", "", fmt.Sprintf("ordering: %s (overrides config sort-repos-by, default %s)", strings.Join(config.ValidReposSort, "|"), config.DefaultReposSort))
	return cmd
}

func runRepos(cmd *cobra.Command, args []string) error {
	entries, err := registry.Load()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no repos tracked yet (run `gg clone <url>` or `gg init`)")
	}
	cfg := config.Load()
	sortKey := cfg.Repos.SortBy
	if v, _ := cmd.Flags().GetString("sort"); v != "" {
		if !slices.Contains(config.ValidReposSort, v) {
			return fmt.Errorf(
				"--sort must be one of: %s",
				strings.Join(config.ValidReposSort, ", "),
			)
		}
		sortKey = v
	}
	sortRepoEntries(entries, sortKey)
	if list, _ := cmd.Flags().GetBool("list"); list {
		for _, e := range entries {
			line := fmt.Sprintf(
				"%s\t%s\t%s\t%s\n",
				e.Name,
				e.Trunk,
				statusLabel(e),
				e.PrimaryWorktree,
			)
			// must be stdout for testing and so that gg repos can be grep'd
			stdout(line)
		}
		return nil
	}
	entry, ok, err := pickRepo(entries, "select a repo")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if s := entry.Validate(); s != registry.StatusOK {
		return fmt.Errorf(
			"%s: %s (run `gg link` from its new location, or `gg cleanup` to drop the entry)",
			entry.Name,
			s,
		)
	}
	stdout(entry.PrimaryWorktree)
	return nil
}

// pickRepo shows a picker of registry entries. Columns: name · trunk ·
// origin · last-used · status. Invalid entries render dimmed with a
// bracketed reason tag. lipgloss/table handles column-width alignment.
// Two parallel tables (base + hover) keep column widths aligned while
// the cursor row recolors to Dirty with the name underlined — matching
// the convention in cd.go / delete.go / stacktree.go.
func pickRepo(entries []registry.Entry, heading string) (registry.Entry, bool, error) {
	// See log.go for the BorderColumn caveat — with outer borders off,
	// BorderColumn(false) silently drops the last row. Keeping the column
	// border at default (hidden char, counts as 1 space) is the workaround.
	mkTable := func() *table.Table {
		return table.New().
			Border(lipgloss.HiddenBorder()).
			BorderTop(false).BorderBottom(false).
			BorderLeft(false).BorderRight(false).
			StyleFunc(func(row, col int) lipgloss.Style {
				// Add extra left padding on the status column so there's a
				// visual break after the more crowded middle columns.
				if col != len(entries) {
					return lipgloss.NewStyle().PaddingRight(1)
				}
				return lipgloss.NewStyle()
			})
	}
	base := mkTable()
	hover := mkTable()
	for _, e := range entries {
		valid := e.Validate() == registry.StatusOK
		nameSty := style.Stderr.Branch
		if !valid {
			nameSty = style.Stderr.Dim
		}
		status := ""
		if !valid {
			status = style.Stderr.Error.Render("(" + statusLabel(e) + ")")
		}
		base.Row(
			nameSty.Render(e.Name),
			style.Stderr.Dim.Render(e.Trunk),
			originLabel(e.Origin),
			style.Stderr.Dim.Render(relativeTime(e.LastUsedAt)),
			status,
		)
		hover.Row(
			style.Stderr.Dirty.Underline(true).Render(e.Name),
			style.Stderr.Dim.Render(e.Trunk),
			originLabel(e.Origin),
			style.Stderr.Dim.Render(relativeTime(e.LastUsedAt)),
			status,
		)
	}
	baseLines := strings.Split(strings.TrimRight(base.String(), "\n"), "\n")
	hoverLines := strings.Split(strings.TrimRight(hover.String(), "\n"), "\n")
	if len(baseLines) != len(entries) || len(hoverLines) != len(entries) {
		return registry.Entry{}, false, fmt.Errorf(
			"internal: rendered %d/%d table rows for %d entries",
			len(baseLines),
			len(hoverLines),
			len(entries),
		)
	}

	items := make([]picker.Item, 0, len(entries))
	for i, e := range entries {
		items = append(items, picker.Item{
			Branch:     e.Bare, // stable identifier (Bare paths don't collide)
			Path:       e.PrimaryWorktree,
			Label:      baseLines[i],
			HoverLabel: hoverLines[i],
			FilterName: e.Name, // fuzzy-match + highlight target
		})
	}
	chosen, ok, err := picker.SelectFiltered(items, heading)
	if err != nil {
		return registry.Entry{}, false, err
	}
	if !ok {
		return registry.Entry{}, false, nil
	}
	for _, e := range entries {
		if e.Bare == chosen.Branch {
			return e, true, nil
		}
	}
	return registry.Entry{}, false, fmt.Errorf("internal: picked entry not found in registry")
}

func originLabel(origin string) string {
	if origin == "" {
		return style.Stderr.Dim.Render("—")
	}
	return style.Stderr.Dim.Render(origin)
}

func statusLabel(e registry.Entry) string {
	s := e.Validate()
	if s == registry.StatusOK {
		return "ok"
	}
	return s.String()
}

// sortRepoEntries reorders entries in place per key. last-used and added
// are time-based with newest first (most-recent activity bubbles up); name
// is alphabetical. Stable so equal keys keep insertion order — matters
// when LastUsedAt is zero on a freshly-added repo that hasn't been cd'd
// into yet.
func sortRepoEntries(entries []registry.Entry, key string) {
	switch key {
	case config.ReposSortName:
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].Name < entries[j].Name
		})
	case config.ReposSortAdded:
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].AddedAt.After(entries[j].AddedAt)
		})
	default: // ReposSortLastUsed
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].LastUsedAt.After(entries[j].LastUsedAt)
		})
	}
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < 90*time.Second:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return strings.TrimSpace(t.Local().Format("2006-01-02"))
	}
}
