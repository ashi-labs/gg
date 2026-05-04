package cli

import (
	"os"
	"strings"

	"github.com/ashi-labs/gg/pkg/config"
	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/gitx/forge"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

// prStatusMsg carries one branch's GetPRStatus result from a fetch goroutine
// to the bubbletea model. err is captured (not surfaced) so a missing PR or
// transient gh failure leaves that row in its fallback render rather than
// killing the whole table.
type prStatusMsg struct {
	branch string
	status forge.PRStatus
	err    error
}

// logProgressModel drives the live `gg log` view. The first frame renders
// every PR cell with a spinner; as prStatusMsg events arrive from the fetch
// goroutines, the model updates its prStatuses map and re-renders. The
// program runs inline (no alt screen) so the final frame stays in normal
// scrollback the moment the program quits — no swap, no tear-down, no
// keypress required to dismiss. Quit fires automatically once every fetch
// has reported in (or immediately when there's nothing to fetch).
type logProgressModel struct {
	rows           []stackRow
	columns        []logColumn
	branchByName   map[string]state.Branch
	latestCommits  map[string]gitx.CommitInfo
	prStatuses     map[string]forge.PRStatus
	pendingFetches int
	spinner        spinner.Model
}

func newLogProgressModel(
	rows []stackRow,
	columns []logColumn,
	branchByName map[string]state.Branch,
	latestCommits map[string]gitx.CommitInfo,
	pending int,
) logProgressModel {
	// No Style on the spinner — renderPRCell wraps the whole `<frame> #<num>`
	// run in Dim so the body and frame stay visually unified. Spinner.Dot's
	// braille frames are single-cell wide, matching the lifecycle glyphs so
	// the cell width stays constant when status pops in.
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return logProgressModel{
		rows:           rows,
		columns:        columns,
		branchByName:   branchByName,
		latestCommits:  latestCommits,
		prStatuses:     make(map[string]forge.PRStatus),
		pendingFetches: pending,
		spinner:        sp,
	}
}

func (m logProgressModel) Init() tea.Cmd {
	// Nothing to wait on → render once and quit immediately. Otherwise kick
	// the spinner so the in-flight `pr` cells animate.
	if m.pendingFetches <= 0 {
		return tea.Quit
	}
	return m.spinner.Tick
}

func (m logProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Manual escape hatch — ctrl+c bails before fetches finish (e.g., a
		// hung forge call). Other keypresses are ignored: we don't want a
		// stray keystroke killing a render the user is actively watching.
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		return m, nil
	case prStatusMsg:
		if msg.err == nil {
			m.prStatuses[msg.branch] = msg.status
		}
		m.pendingFetches--
		if m.pendingFetches <= 0 {
			return m, tea.Quit
		}
		return m, nil
	case spinner.TickMsg:
		// Stop ticking once every fetch has reported in — the spinner
		// frame isn't read by any cell at that point, so further
		// re-renders would just churn CPU.
		if m.pendingFetches <= 0 {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m logProgressModel) View() string {
	// Last frame is intentionally empty. Bubbletea's inline mode clears the
	// rendered region on quit, and trailing lines of the prior frame can be
	// truncated if we leave the table in place — so we paint a blank final
	// frame to leave the cursor in a clean state, then renderLogTableLive
	// re-prints the finalized table to stdout post-Run for scrollback.
	if m.pendingFetches <= 0 {
		return ""
	}
	// bubbles' spinner.Dot frames each carry a trailing space so they
	// space themselves nicely when used standalone. renderPRCell already
	// adds its own " " separator, so trim to keep the cell width matched
	// to the lifecycle-glyph render (`<glyph> #<num>` = 5 cells).
	frame := strings.TrimRight(m.spinner.View(), " ")
	return renderLogTable(m.rows, m.columns, m.branchByName, m.latestCommits, m.prStatuses, frame)
}

// shouldRenderLogLive returns true when the bubbletea-driven view can run.
// The only requirement is that stdout is a TTY — the inline re-paint relies
// on cursor control codes a real terminal supports, and piped/grep'd output
// must keep its synchronous, scrollable behavior. Whether or not there are
// PRs to fetch, every TTY invocation goes through the same live view so the
// UX is consistent.
func shouldRenderLogLive() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}

// renderLogTableLive runs the log view under a bubbletea program in inline
// mode (no alt screen). cachedPRStatuses is the set of PR statuses already
// known at render time (read from the on-disk cache); branches present
// there don't trigger a live fetch. The table re-paints in place as the
// remaining live fetches return; when every fetch has reported in (or
// there were none to begin with) the program quits and the final frame is
// re-printed to stdout as scrollback. ctrl+c bails out early if a fetch
// hangs.
//
// After the program exits, the merged set of statuses (cache + live) is
// written back to the on-disk cache so the next invocation finds them
// fresh — this is also how the synchronous prefetch path keeps the cache
// up to date when a user runs `gg log` directly without a precmd hook.
func renderLogTableLive(
	rows []stackRow,
	columns []logColumn,
	branchByName map[string]state.Branch,
	latestCommits map[string]gitx.CommitInfo,
	branches []state.Branch,
	cachedPRStatuses map[string]forge.PRStatus,
) error {
	// Only count branches whose PR status we can actually fetch. With no
	// forge wired up (or no `pr` column requested), pending stays 0 — Init
	// returns tea.Quit immediately and the model paints exactly once.
	wantsPR := false
	for _, c := range columns {
		if c.name == config.LogColumnPR {
			wantsPR = true
			break
		}
	}
	canFetch := wantsPR && gitx.Forge != nil
	pending := 0
	if canFetch {
		for _, b := range branches {
			if b.PRNumber <= 0 || b.Worktree == "" {
				continue
			}
			if _, hit := cachedPRStatuses[b.Name]; hit {
				continue
			}
			pending++
		}
	}
	m := newLogProgressModel(rows, columns, branchByName, latestCommits, pending)
	// Seed the model with cached statuses so the first paint already shows
	// resolved PR cells where the cache had a hit — no spinner flicker for
	// rows that the cache covers.
	for n, s := range cachedPRStatuses {
		m.prStatuses[n] = s
	}
	p := tea.NewProgram(m)

	// Fetch goroutines live for the duration of the program. Send is
	// safe before Run is called — bubbletea queues messages until the
	// event loop starts. Errors are folded into the message so the model
	// can still decrement pendingFetches cleanly. We only fetch branches
	// missing from the cache; cached entries are already in m.prStatuses.
	if canFetch {
		for _, b := range branches {
			if b.PRNumber <= 0 || b.Worktree == "" {
				continue
			}
			if _, hit := cachedPRStatuses[b.Name]; hit {
				continue
			}
			go func(b state.Branch) {
				s, err := gitx.Forge.GetPRStatus(b.Worktree, b.PRNumber)
				p.Send(prStatusMsg{branch: b.Name, status: s, err: err})
			}(b)
		}
	}

	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	// View() returns "" for the final frame so bubbletea doesn't drop any
	// lines on inline-cleanup. Now that the program has fully exited, re-emit
	// the finalized table (with whatever PR statuses arrived) to stdout so it
	// lands in normal scrollback like any other command's output.
	if fm, ok := finalModel.(logProgressModel); ok {
		stdout(renderLogTable(
			fm.rows,
			fm.columns,
			fm.branchByName,
			fm.latestCommits,
			fm.prStatuses,
			"",
		))
		// Refresh the on-disk cache with whatever the live fetches resolved.
		// Cache writes are best-effort: a failure here just means the next
		// `gg log` will refetch — no user-visible breakage.
		writePRCacheFromModel(branches, fm.prStatuses)
	}
	return nil
}
