package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/ashi-labs/gg/pkg/sync"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

// branchPhase is the per-branch display state the sync TUI tracks. The
// sync engine emits events (EventBranchStart, EventBranchDone, …) that
// transition a row through these phases.
type branchPhase int

const (
	phasePending branchPhase = iota
	phaseRunning
	phaseDone
	phaseSkipped
	phaseFailed
)

// syncMsg wraps a sync.Event delivered from the engine goroutine to the
// bubbletea model via Program.Send.
type syncMsg sync.Event

// syncDoneMsg signals the engine goroutine has returned. err is the
// final sync error (nil on success). The model uses this to render the
// final frame and exit cleanly.
type syncDoneMsg struct{ err error }

// ttySuspender runs fn with the active progress UI's grip on the TTY
// released, so an interactive prompt (huh) can read stdin without
// fighting bubbletea for keystrokes — the visible symptom of that
// fight is having to press Enter twice. Under the plain (non-TTY)
// runner this is a passthrough.
type ttySuspender func(fn func() error) error

// preRebaseHook runs after the engine emits EventFetchDone and before
// the rebase plan is built. The CLI uses it to prune forge-merged
// branches in line with the progress UI; the engine itself doesn't know
// about forge concepts. Receives the same emit closure the engine uses,
// so any events the hook fires render in the same frame as fetch/rebase,
// plus a ttySuspender to wrap any interactive prompt.
type preRebaseHook func(emit func(sync.Event) error, suspend ttySuspender) error

// teaSuspender returns a ttySuspender that releases p's terminal grip
// for the duration of fn and restores it after, so a huh form (or any
// other line-discipline reader) can own stdin uncontested. Restore
// failures don't mask fn's error.
func teaSuspender(p *tea.Program) ttySuspender {
	return func(fn func() error) error {
		if err := p.ReleaseTerminal(); err != nil {
			return err
		}
		fnErr := fn()
		if err := p.RestoreTerminal(); err != nil && fnErr == nil {
			return err
		}
		return fnErr
	}
}

// passthroughSuspender is the no-op ttySuspender used by the plain
// (non-TTY) runner — there's no progress UI to release.
func passthroughSuspender(fn func() error) error { return fn() }

// runSyncWithProgress runs sync.Run under a bubbletea program that
// renders a live per-branch progress view. The engine runs in a
// goroutine and pushes Event values at the program; the model flips
// rows between phases as each event arrives.
//
// preRebase is invoked from inside the engine's emit chain on
// EventFetchDone, so any events it emits (e.g. EventBranchPruned for a
// merged-PR cleanup) flow through the same forwarder and render between
// the fetch row and the branch rows. nil is allowed for paths with no
// pre-rebase work.
//
// When stderr isn't a TTY (CI, test containers, redirected shells) we
// short-circuit to runSyncPlain — bubbletea tries to open /dev/tty for
// input on launch and errors out in those environments otherwise.
//
// Output goes to stderr so commands like `gg sync | cat` don't get the
// progress frames interleaved with any future stdout writes.
func runSyncWithProgress(opts sync.RunOpts, title string, preRebase preRebaseHook) error {
	if !isatty.IsTerminal(os.Stderr.Fd()) {
		return runSyncPlain(opts, title, preRebase)
	}
	m := newSyncProgressModel(title)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	var runErr error
	go func() {
		// Forward every event to tea, then run preRebase on FetchDone.
		// emit is captured by reference so preRebase can re-enter it
		// (e.g. the prune emits EventBranchPruned which lands back in
		// this same closure and on through to tea).
		suspend := teaSuspender(p)
		var emit func(e sync.Event) error
		emit = func(e sync.Event) error {
			p.Send(syncMsg(e))
			if e.Kind == sync.EventFetchDone && preRebase != nil {
				return preRebase(emit, suspend)
			}
			return nil
		}
		opts.OnEvent = emit
		runErr = sync.Run(
			sync.Repo{
				PrimaryWorktree: repo.PrimaryWorktree,
				Trunk:           repo.Trunk,
				BareDir:         bare,
			},
			opts,
		)
		p.Send(syncDoneMsg{err: runErr})
	}()
	if _, err := p.Run(); err != nil {
		return err
	}
	return runErr
}

// runContinueWithProgress mirrors runSyncWithProgress but drives
// sync.Continue instead of sync.Run. Shares the same TUI model + plain
// fallback so a paused-then-resumed flow looks continuous. No preRebase
// hook — continue picks up after the fetch phase has already happened.
func runContinueWithProgress(opts sync.RunOpts, title string) error {
	if !isatty.IsTerminal(os.Stderr.Fd()) {
		return runContinuePlain(opts, title)
	}
	m := newSyncProgressModel(title)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	var runErr error
	go func() {
		opts.OnEvent = func(e sync.Event) error {
			p.Send(syncMsg(e))
			return nil
		}
		runErr = sync.Continue(
			sync.Repo{
				PrimaryWorktree: repo.PrimaryWorktree,
				Trunk:           repo.Trunk,
				BareDir:         bare,
			},
			opts,
		)
		p.Send(syncDoneMsg{err: runErr})
	}()
	if _, err := p.Run(); err != nil {
		return err
	}
	return runErr
}

func runContinuePlain(opts sync.RunOpts, title string) error {
	if title != "" {
		plainln(style.Stderr.Hint.Render(title))
	}
	opts.OnEvent = func(e sync.Event) error {
		switch e.Kind {
		case sync.EventBranchStart:
			plainln(
				style.Stderr.Dim.Render(
					"  rebasing ",
				) + styleBranch(
					e.Branch,
				) + style.Stderr.Dim.Render(
					"…",
				),
			)
		case sync.EventBranchDone:
			successf("%s rebased", styleBranch(e.Branch))
		case sync.EventBranchSkipped:
			plainln(
				style.Stderr.Dim.Render(
					"· ",
				) + styleBranch(
					e.Branch,
				) + style.Stderr.Dim.Render(
					" (up to date)",
				),
			)
		case sync.EventBranchFailed:
			if e.Err != nil {
				errorf("%s: %s", styleBranch(e.Branch), firstLine(e.Err.Error()))
			} else {
				errorf("%s failed", styleBranch(e.Branch))
			}
		}
		return nil
	}
	err := sync.Continue(sync.Repo{
		PrimaryWorktree: repo.PrimaryWorktree,
		Trunk:           repo.Trunk,
		BareDir:         bare,
	}, opts)
	if err == nil {
		successf("sync complete")
	}
	return err
}

// runSyncPlain is the non-TTY fallback. It emits a one-line status per
// engine event to stderr and returns the sync error unchanged. Keeps
// CI and piped invocations quiet-but-useful instead of dying on the
// missing /dev/tty open.
func runSyncPlain(opts sync.RunOpts, title string, preRebase preRebaseHook) error {
	if title != "" {
		plainln(style.Stderr.Hint.Render(title))
	}
	var emit func(e sync.Event) error
	emit = func(e sync.Event) error {
		switch e.Kind {
		case sync.EventFetchStart:
			plainln(style.Stderr.Dim.Render("  fetching origin…"))
		case sync.EventFetchDone:
			successf("fetched origin")
			if preRebase != nil {
				if err := preRebase(emit, passthroughSuspender); err != nil {
					return err
				}
			}
		case sync.EventBranchStart:
			plainln(
				style.Stderr.Dim.Render(
					"  rebasing ",
				) + styleBranch(
					e.Branch,
				) + style.Stderr.Dim.Render(
					"…",
				),
			)
		case sync.EventBranchDone:
			successf("%s rebased", styleBranch(e.Branch))
		case sync.EventBranchSkipped:
			plainln(
				style.Stderr.Dim.Render(
					"· ",
				) + styleBranch(
					e.Branch,
				) + style.Stderr.Dim.Render(
					" (up to date)",
				),
			)
		case sync.EventBranchFailed:
			if e.Err != nil {
				errorf("%s: %s", styleBranch(e.Branch), firstLine(e.Err.Error()))
			} else {
				errorf("%s failed", styleBranch(e.Branch))
			}
		case sync.EventBranchPruned:
			if e.Detail != "" {
				successf("pruned %s (%s)", styleBranch(e.Branch), e.Detail)
			} else {
				successf("pruned %s", styleBranch(e.Branch))
			}
		}
		return nil
	}
	opts.OnEvent = emit
	err := sync.Run(sync.Repo{
		PrimaryWorktree: repo.PrimaryWorktree,
		Trunk:           repo.Trunk,
		BareDir:         bare,
	}, opts)
	if err == nil {
		successf("sync complete")
	}
	return err
}

// prunedRow captures one branch removed by the preRebase hook so the
// progress UI can render it between the fetch row and the branch rows.
type prunedRow struct {
	name   string
	detail string // free-form annotation, e.g. "PR #13 merged"
}

// syncProgressModel is the bubbletea model for the sync TUI.
type syncProgressModel struct {
	title     string
	branches  []string
	phase     map[string]branchPhase
	phaseErr  map[string]error
	fetching  bool
	fetchDone bool
	pruned    []prunedRow
	finished  bool
	finalErr  error
	spinner   spinner.Model
}

func newSyncProgressModel(title string) syncProgressModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Stderr.Dirty
	return syncProgressModel{
		title:    title,
		phase:    make(map[string]branchPhase),
		phaseErr: make(map[string]error),
		spinner:  sp,
	}
}

func (m syncProgressModel) Init() tea.Cmd { return m.spinner.Tick }

func (m syncProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case syncMsg:
		return m.applyEvent(sync.Event(msg)), nil
	case syncDoneMsg:
		m.finished = true
		m.finalErr = msg.err
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

// applyEvent transitions model state in response to one engine event.
// Kept as a method taking + returning a value so Update's switch stays
// compact.
func (m syncProgressModel) applyEvent(e sync.Event) syncProgressModel {
	switch e.Kind {
	case sync.EventFetchStart:
		m.fetching = true
	case sync.EventFetchDone:
		m.fetching = false
		m.fetchDone = true
	case sync.EventPlan:
		m.branches = append([]string(nil), e.Branches...)
		for _, b := range m.branches {
			m.phase[b] = phasePending
		}
	case sync.EventBranchStart:
		m.phase[e.Branch] = phaseRunning
	case sync.EventBranchDone:
		m.phase[e.Branch] = phaseDone
	case sync.EventBranchSkipped:
		m.phase[e.Branch] = phaseSkipped
	case sync.EventBranchFailed:
		m.phase[e.Branch] = phaseFailed
		if e.Err != nil {
			m.phaseErr[e.Branch] = e.Err
		}
	case sync.EventBranchPruned:
		m.pruned = append(m.pruned, prunedRow{name: e.Branch, detail: e.Detail})
	}
	return m
}

func (m syncProgressModel) View() string {
	var b strings.Builder
	if m.title != "" {
		b.WriteString(style.Stderr.Hint.Render(m.title))
		b.WriteString("\n\n")
	}
	// Fetch row (only shown when relevant).
	switch {
	case m.fetching:
		b.WriteString(m.spinner.View())
		b.WriteString(" ")
		b.WriteString("fetching origin…")
		b.WriteString("\n")
	case m.fetchDone:
		b.WriteString(style.Stderr.Success.Render(style.Glyphs.Success))
		b.WriteString(" fetched origin\n")
	}
	// Pruned rows — fired between fetch and rebase by the preRebase hook
	// (typically merged-PR cleanup). Rendered with the success glyph in
	// the same style as a finished branch rebase.
	for _, p := range m.pruned {
		b.WriteString(style.Stderr.Success.Render(style.Glyphs.Success))
		b.WriteString(" pruned ")
		b.WriteString(style.Stderr.Branch.Render(p.name))
		if p.detail != "" {
			b.WriteString("  ")
			b.WriteString(style.Stderr.Dim.Render("(" + p.detail + ")"))
		}
		b.WriteByte('\n')
	}
	// Branch rows.
	for _, name := range m.branches {
		b.WriteString(m.renderBranchRow(name))
		b.WriteByte('\n')
	}
	// Success summary. On failure we leave the per-row ✗ to tell the
	// story — cobra's Execute() prints the engine error right after the
	// TUI exits, so a second summary here just double-stamps.
	if m.finished && m.finalErr == nil {
		b.WriteString(style.Stderr.Success.Render(style.Glyphs.Success) + " sync complete")
		b.WriteByte('\n')
	}
	return b.String()
}

// renderBranchRow picks the glyph + color for a single branch based on
// its current phase and returns "<glyph> <name>" (plus an inline error
// trail if failed).
func (m syncProgressModel) renderBranchRow(name string) string {
	ph := m.phase[name]
	var glyph, label string
	nameSty := style.Stderr.Branch
	switch ph {
	case phasePending:
		glyph = style.Stderr.Dim.Render(style.Glyphs.Pending)
		nameSty = style.Stderr.Dim
	case phaseRunning:
		glyph = m.spinner.View()
		nameSty = style.Stderr.Dirty
	case phaseDone:
		glyph = style.Stderr.Success.Render(style.Glyphs.Success)
	case phaseSkipped:
		glyph = style.Stderr.Dim.Render(style.Glyphs.Skipped)
		nameSty = style.Stderr.Dim
		label = "  " + style.Stderr.Dim.Render("(up to date)")
	case phaseFailed:
		glyph = style.Stderr.Error.Render(style.Glyphs.Failure)
		nameSty = style.Stderr.Error
		if err, ok := m.phaseErr[name]; ok && err != nil {
			label = "  " + style.Stderr.Dim.Render(firstLine(err.Error()))
		}
	}
	return fmt.Sprintf("%s %s%s", glyph, nameSty.Render(name), label)
}

// firstLine trims to the first usable line of s — git rebase errors
// tend to be multi-line and often contain \r progress bytes ("Rebasing
// (1/1)\r") that, if left in, get interpreted by the terminal and
// scramble our row. We normalise \r to \n so the split is clean, drop
// empty leading lines (git sometimes emits a blank line first), and
// skip a leading "Rebasing (N/M)" progress line if it's still the
// first token.
func firstLine(s string) string {
	s = strings.ReplaceAll(s, "\r", "\n")
	for ln := range strings.SplitSeq(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "Rebasing (") {
			continue
		}
		return ln
	}
	return strings.TrimSpace(s)
}
