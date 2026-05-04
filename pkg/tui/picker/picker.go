// Package picker offers two interactive choosers for the CLI:
//
//   - Select: a minimal cursor-driven picker used by `gg checkout`, where
//     the item count is bounded by stack depth (typically 3-6). No filter,
//     no pagination — just arrows + enter.
//   - SelectFiltered: a bubbles/list-backed picker used by `gg repos` and
//     `gg cd`, where the item count can grow arbitrarily (many repos, or
//     branches across many stacks). Adds `/` fuzzy filter on top.
//
// Both share the same Item type and "print the chosen path on stdout" shell
// contract. I/O is pinned to /dev/tty so the picker can draw while our
// stdout is captured by the shell wrapper for `cd`.
package picker

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ashi-labs/gg/pkg/config"
	"github.com/ashi-labs/gg/pkg/tui/style"
)

// resolveAlign returns the configured full-screen alignment or the
// default. Unknown values fall back with a one-line stderr warning so
// typos don't silently revert to the default.
func resolveAlign() string {
	v := config.Load().Prompt.Location
	switch v {
	case config.PromptLocationTop, config.PromptLocationBottom:
		return v
	case "":
		return config.DefaultPromptLocation
	default:
		fmt.Fprintf(os.Stderr,
			"gg: unknown terminal-align %q, falling back to %q (valid: %q, %q)\n",
			v, config.DefaultPromptLocation,
			config.PromptLocationTop, config.PromptLocationBottom)
		return config.DefaultPromptLocation
	}
}

// Item is one row in either picker. Label and HoverLabel are pre-rendered
// by the caller; FilterName is the short plain-text string used for fuzzy
// matching and for match-highlight rendering when the picker is filtering.
// If FilterName is empty, Branch is used.
type Item struct {
	Branch     string
	Path       string
	Label      string // fully rendered display line (normal mode)
	HoverLabel string // cursor-row variant (e.g., name underlined)
	// FilterName is the plain text that gets fuzzy-matched. Must not
	// contain ANSI codes — the delegate wraps matched runes in our
	// highlight style during filtering, which requires clean input.
	FilterName string
	// IsTrunk marks the item as the trunk branch. Drives the base color
	// during filter mode (Trunk vs Branch) since the pre-rendered Label
	// isn't used there.
	IsTrunk bool
	// IsRepo marks the item as a repository anchor (used by `gg cd`'s
	// multi-repo tree). Takes Repo color in filter mode.
	IsRepo bool
	// Current marks the currently-checked-out branch (or the "active"
	// row). Same reason as IsTrunk — lets the delegate reconstitute
	// colors during filter mode where Label is replaced.
	Current bool
}

// FilterValue satisfies list.Item for fuzzy matching in SelectFiltered.
func (i Item) FilterValue() string {
	if i.FilterName != "" {
		return i.FilterName
	}
	return i.Branch
}

// openTTY resolves the shared TTY handle used by both pickers. Returning an
// error up front is friendlier than surfacing a bubbletea panic on headless
// shells.
func openTTY() (*os.File, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("picker requires a TTY (open /dev/tty: %w)", err)
	}
	return tty, nil
}

func currentIdx(items []Item) int {
	for i, it := range items {
		if it.Current {
			return i
		}
	}
	return 0
}

// ─────────────────────────────────────────────────────────────────────────
// Select — simple cursor picker (gg checkout)
// ─────────────────────────────────────────────────────────────────────────

// Select launches a minimal cursor-driven picker. Best when the item count
// is small and known (stack-local branches).
func Select(items []Item, heading string) (Item, bool, error) {
	if len(items) == 0 {
		return Item{}, false, fmt.Errorf("no items to pick from")
	}
	tty, err := openTTY()
	if err != nil {
		return Item{}, false, err
	}
	defer tty.Close()

	m := simpleModel{
		items:   items,
		heading: heading,
		cursor:  currentIdx(items),
		chosen:  -1,
		align:   resolveAlign(),
	}
	p := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return Item{}, false, err
	}
	fm := final.(simpleModel)
	if fm.chosen < 0 {
		return Item{}, false, nil
	}
	return fm.items[fm.chosen], true, nil
}

type simpleModel struct {
	items   []Item
	heading string
	cursor  int
	chosen  int
	// height tracks the alt-screen canvas height (from WindowSizeMsg) so
	// View can bottom-anchor the rendering the same way the filtered
	// picker does.
	height int
	// align is the user's chosen content anchor within the alt-screen
	// canvas (userconfig.TerminalAlignTop / ...Bottom).
	align string
}

func (m simpleModel) Init() tea.Cmd { return nil }

func (m simpleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.height = ws.Height
		return m, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "ctrl+c", "q", "esc":
			m.chosen = -1
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "home", "g":
			m.cursor = 0
		case "end", "G":
			m.cursor = len(m.items) - 1
		case "enter":
			m.chosen = m.cursor
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m simpleModel) View() string {
	// Build line-by-line and join at the end so we don't end with a
	// trailing newline — that final newline would otherwise show up as
	// an extra blank row in the alt-screen rendering.
	var lines []string
	if m.heading != "" {
		lines = append(lines, style.Stderr.Hint.Render(m.heading), "")
	}
	for i, it := range m.items {
		if i == m.cursor {
			lines = append(lines, it.HoverLabel+"  "+style.Stderr.Dirty.Render(style.Glyphs.Caret))
		} else {
			lines = append(lines, it.Label)
		}
	}
	lines = append(lines, "", style.Stderr.Dim.Render("↑/↓ navigate · enter select · esc cancel"))
	return anchorBottom(strings.Join(lines, "\n"), m.height, m.align)
}

// anchorBottom returns `body` as-is when top-aligned or when we don't
// know the canvas height yet. When bottom-aligned and a height is known
// it pads the space above `body` with blank rows so its last line lands
// on the last terminal row. Shared between simpleModel and listModel.
func anchorBottom(body string, height int, align string) string {
	if align != config.PromptLocationBottom || height <= 0 {
		return body
	}
	used := lipgloss.Height(body)
	needed := height - used
	if needed <= 0 {
		return body
	}
	return strings.Repeat("\n", needed) + body
}

// ─────────────────────────────────────────────────────────────────────────
// SelectFiltered — bubbles/list with fuzzy search (gg repos, gg cd)
// ─────────────────────────────────────────────────────────────────────────

// Layout constants for the bubbles/list picker.
//
// listChromeRows is the vertical space the list's chrome consumes above
// and below the visible items. Our listStyles gives TitleBar 1 row of
// bottom padding (= 2 total including the title/filter text) and
// HelpStyle 1 row of top padding (= 2 total including the help text).
// Status bar and pagination are both disabled. Total = 4.
//
// Choosing exactly 4 matters: bubbles/list pads the content area to
// fill `availHeight = m.height - chrome`. If chrome is too small, that
// padding creates extra blank rows between the last item and the help
// legend. If too large, items get truncated.
//
// initialWidth is a placeholder for the first render — WindowSizeMsg
// replaces it as soon as alt-screen starts.
const (
	listChromeRows = 4
	initialWidth   = 80
)

// SelectFiltered launches a bubbles/list-backed picker with `/` fuzzy
// search, scrolling, and richer keybindings. Best when the item count can
// be large (repo inventory, cross-stack branch pool).
func SelectFiltered(items []Item, heading string) (Item, bool, error) {
	if len(items) == 0 {
		return Item{}, false, fmt.Errorf("no items to pick from")
	}
	tty, err := openTTY()
	if err != nil {
		return Item{}, false, err
	}
	defer tty.Close()

	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	// Initial width/height are placeholders; WindowSizeMsg replaces them
	// on the first frame with the real alt-screen dimensions.
	l := list.New(listItems, listDelegate{}, initialWidth, len(items)+listChromeRows)
	l.Title = heading
	l.SetShowTitle(heading != "")
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetShowHelp(true)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	l.Styles = listStyles()
	// The help bubble carries its own Styles for keybinding + description
	// text, independent of list's HelpStyle (which only frames the block).
	// Without this override the legend renders in the terminal's default
	// foreground instead of our dim palette.
	l.Help.Styles = helpStyles()
	// The filter textinput was constructed by list.New before our Styles
	// override landed, so the initial "Filter:" prompt style is still the
	// bubbles default. Re-assign after the fact to pick up our palette.
	l.FilterInput.PromptStyle = style.Stderr.Hint
	l.FilterInput.TextStyle = style.Stderr.Foreground
	l.FilterInput.Cursor.Style = style.Stderr.Dirty
	l.Select(currentIdx(items))
	skipUnselectable(&l, 1)

	m := listModel{
		list:   l,
		chosen: -1,
		align:  resolveAlign(),
	}
	p := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return Item{}, false, err
	}
	fm := final.(listModel)
	if fm.chosen < 0 || fm.chosen >= len(items) {
		return Item{}, false, nil
	}
	return items[fm.chosen], true, nil
}

type listModel struct {
	list   list.Model
	chosen int
	// width/height of the alt-screen. WindowSizeMsg fills these in on
	// startup and on terminal resize. Until we get one, they're 0 and
	// setListHeight falls back to a compact size.
	width  int
	height int
	// align is the user's chosen content anchor (see resolveAlign).
	align string
}

func (m listModel) Init() tea.Cmd { return nil }

func (m listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Grab terminal size when bubbletea reports it. Alt-screen mode gives
	// us a full-frame canvas, so we lock the list to that height and pin
	// the help legend to the bottom of the screen in View().
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = ws.Width
		m.height = ws.Height
		m.list.SetWidth(ws.Width)
		m.list.SetHeight(ws.Height)
		// Skip forwarding — list.Model would recompute its own height
		// based on our overrides, which is what we want.
	}

	// Direction hint for the post-update skip logic. Any key that could
	// land the cursor on an unselectable row (incl. filter typing, which
	// reshuffles visible items) sets this so skipUnselectable knows which
	// way to hop.
	dir := 0

	if km, ok := msg.(tea.KeyMsg); ok {
		filtering := m.list.FilterState() == list.Filtering
		if filtering {
			// fzf-style: while typing the filter, Up/Down still move the
			// list cursor and Enter selects the cursor item directly (no
			// need to first commit the filter with Enter and then press
			// Enter again). ctrl+c cancels the whole picker; esc falls
			// through to list.Model which clears the filter.
			switch km.String() {
			case "ctrl+c":
				m.chosen = -1
				return m, tea.Quit
			case "up":
				m.list.CursorUp()
				skipUnselectable(&m.list, -1)
				m.setListHeight()
				return m, nil
			case "down":
				m.list.CursorDown()
				skipUnselectable(&m.list, 1)
				m.setListHeight()
				return m, nil
			case "enter":
				if it, ok := m.list.SelectedItem().(Item); ok && !it.IsRepo {
					m.chosen = indexOf(m.list.Items(), it)
					return m, tea.Quit
				}
			}
			// Anything else (typing) re-runs the filter — need to re-snap
			// the cursor off any now-selected repo afterwards.
			dir = 1
		} else {
			switch km.String() {
			case "ctrl+c", "esc", "q":
				m.chosen = -1
				return m, tea.Quit
			case "enter":
				if it, ok := m.list.SelectedItem().(Item); ok && !it.IsRepo {
					m.chosen = indexOf(m.list.Items(), it)
					return m, tea.Quit
				}
			case "up", "k", "end", "G":
				dir = -1
			case "down", "j", "home", "g":
				dir = 1
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	if dir != 0 {
		skipUnselectable(&m.list, dir)
	}
	// Shrink the list's height to match the visible count so the block
	// doesn't leave phantom empty rows while filtering narrows results.
	m.setListHeight()
	return m, cmd
}

// skipUnselectable advances the cursor past rows that shouldn't be
// picked (currently: repo anchors in the `gg cd` tree). It first tries
// `dir`; if it hits a boundary without finding a selectable row it
// falls back to the other direction from the original position. No-ops
// when the current row is already selectable.
func skipUnselectable(m *list.Model, dir int) {
	if cur, ok := m.SelectedItem().(Item); !ok || !cur.IsRepo {
		return
	}
	if dir == 0 {
		dir = 1
	}
	start := m.Index()
	for _, d := range [...]int{dir, -dir} {
		m.Select(start)
		for steps := 0; steps < len(m.VisibleItems()); steps++ {
			prev := m.Index()
			if d > 0 {
				m.CursorDown()
			} else {
				m.CursorUp()
			}
			if m.Index() == prev {
				break // hit boundary
			}
			if cur, ok := m.SelectedItem().(Item); ok && !cur.IsRepo {
				return
			}
		}
	}
	// All items are unselectable — leave the cursor where it started.
	m.Select(start)
}

// setListHeight picks the right height for the list. With alt-screen
// enabled (which we always use now), we lock to the full terminal height
// the moment bubbletea hands us a WindowSizeMsg — that gives the list a
// "proper TUI" frame where the title anchors at the top and the help
// legend at the bottom, matching how fzf/less/etc. feel.
//
// Before the first WindowSizeMsg arrives the size is unknown, so we fall
// back to a compact height that just fits the visible items + chrome.
// That initial frame is invisible to the user (alt-screen swap happens
// after the first resize message) but keeps layout sensible if the msg
// never comes.
// setListHeight picks the tightest height that still fits the current
// chrome + visible item count. We intentionally do not expand to the
// full terminal height here — View() handles bottom-anchoring by
// prepending blank rows to the compact rendering, which is simpler than
// convincing list.Model's internal layout to align to the bottom.
func (m *listModel) setListHeight() {
	n := len(m.list.VisibleItems())
	if n < 1 {
		n = 1
	}
	m.list.SetHeight(n + listChromeRows)
}

func (m listModel) View() string {
	return anchorBottom(m.list.View(), m.height, m.align)
}

// indexOf locates the chosen Item in the full (unfiltered) slice so the
// returned index is stable regardless of filter state.
func indexOf(all []list.Item, target Item) int {
	for i, it := range all {
		if ti, ok := it.(Item); ok && ti.Branch == target.Branch {
			return i
		}
	}
	return -1
}

type listDelegate struct{}

func (listDelegate) Height() int                             { return 1 }
func (listDelegate) Spacing() int                            { return 0 }
func (listDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (listDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	it, ok := listItem.(Item)
	if !ok {
		return
	}
	isCursor := index == m.Index()
	filtering := m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied

	var line string
	if filtering && it.FilterValue() != "" {
		// During filter mode the columnar pre-rendered Label can't be
		// per-rune restyled (ANSI codes throw off rune positions), so we
		// render a simpler plain-text row with the matched runes
		// underlined in the Dirty accent. Base color follows the same
		// rules as non-filter mode so the picker stays self-consistent:
		// cursor → Dirty (matches the caret), trunk → Trunk, current →
		// Current, everything else → Branch.
		base := style.Stderr.Branch
		switch {
		case isCursor:
			base = style.Stderr.Dirty
		case it.Current:
			base = style.Stderr.Current
		case it.IsTrunk:
			base = style.Stderr.Trunk
		case it.IsRepo:
			base = style.Stderr.Repo
		}
		matched := style.Stderr.Dirty.Underline(true)
		line = lipgloss.StyleRunes(it.FilterValue(), m.MatchesForItem(index), matched, base)
	} else if isCursor {
		line = it.HoverLabel
	} else {
		line = it.Label
	}
	if isCursor {
		line += "  " + style.Stderr.Dirty.Render(style.Glyphs.Caret)
	}
	fmt.Fprint(w, line)
}

// listStyles overrides bubbles/list's contrast-heavy defaults (big magenta
// title badge, magenta filter cursor) with our palette. Reuse the prebuilt
// palette styles directly rather than synthesizing new ones — the palette
// styles are bound to the stderr renderer, which carries the correct color
// profile for this stream. Synthesizing via lipgloss.NewStyle() produces
// styles bound to lipgloss's default renderer and can render differently.
func listStyles() list.Styles {
	s := list.DefaultStyles()
	s.Title = style.Stderr.Hint
	s.TitleBar = lipgloss.NewStyle().Padding(0, 0, 1, 0)
	s.FilterPrompt = style.Stderr.Hint
	s.FilterCursor = style.Stderr.Dirty
	s.DefaultFilterCharacterMatch = style.Stderr.Dirty.Underline(true)
	s.HelpStyle = lipgloss.NewStyle().PaddingTop(1)
	return s
}

// helpStyles re-skins the help bubble inside list.Model with our dim /
// hint palette so the bottom legend matches the rest of the picker.
func helpStyles() help.Styles {
	s := help.Styles{
		ShortKey:       style.Stderr.Dim,
		ShortDesc:      style.Stderr.Dim,
		ShortSeparator: style.Stderr.Dim,
		Ellipsis:       style.Stderr.Dim,
		FullKey:        style.Stderr.Dim,
		FullDesc:       style.Stderr.Dim,
		FullSeparator:  style.Stderr.Dim,
	}
	return s
}
