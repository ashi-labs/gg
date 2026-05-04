package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/gitx/forge"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

func newSubmitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Open/refresh the current branch's PR on GitHub (or the whole stack with --all).",
		Args:  cobra.NoArgs,
		RunE:  runSubmit,
	}
	cmd.Flags().BoolP("all", "a", false, "submit every branch in the current stack")
	cmd.Flags().Bool("upstack", false, "submit current branch and descendants")
	cmd.Flags().Bool("downstack", false, "submit current branch and ancestors")
	cmd.Flags().Bool("draft", false, "create PRs in draft state")
	cmd.Flags().
		String("title", "", "PR title (overrides the auto-derived first-commit subject; single-branch only)")
	cmd.Flags().
		String("body", "", "PR body (replaces the template-seeded body; stack footer is still appended; single-branch only)")
	return cmd
}

func runSubmit(cmd *cobra.Command, args []string) error {
	if repo == nil {
		return ctxResolutionErr
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not installed; see https://cli.github.com/")
	}
	current, err := gitx.Revision.CurrentBranch(cwd)
	if err != nil {
		return err
	}
	if current == "HEAD" || current == "" {
		return fmt.Errorf("detached HEAD; checkout a branch first")
	}
	if current == repo.Trunk {
		return fmt.Errorf("cannot submit trunk (%s)", repo.Trunk)
	}

	branches, err := state.AllBranches(bare)
	if err != nil {
		return err
	}
	lineage := stack.Build(repo.Trunk, branches)
	if !lineage.Contains(current) {
		return fmt.Errorf("%s is not tracked (run `gg track` first)", current)
	}

	scope := resolveSubmitScope(cmd)
	selected := lineage.Select(scope, current)
	ordered := topoFilter(lineage, selected)
	draft, _ := cmd.Flags().GetBool("draft")
	titleFlag, _ := cmd.Flags().GetString("title")
	bodyFlag, _ := cmd.Flags().GetString("body")
	bodyFlagSet := cmd.Flags().Changed("body")
	// --title/--body apply per-PR. Sharing the same string across an
	// entire stack would land identical titles on multiple PRs, which
	// is almost never what the caller wants — flag it loudly.
	if (titleFlag != "" || bodyFlagSet) && scope != stack.ScopeBranch {
		return fmt.Errorf("--title/--body only apply when submitting a single branch (drop --all/--upstack/--downstack)")
	}
	// Pass 1: create any missing PRs in topological order so each child's
	// parent branch is already on origin by the time gh creates its PR.
	prs := map[string]int{}
	createdPR := false
	pushedCommits := false
	for _, name := range ordered {
		b, err := state.LoadBranch(bare, name)
		if err != nil {
			return err
		}
		localSHA, _ := gitx.Revision.HeadSHA(b.Worktree, name)
		remoteSHA, _ := gitx.Revision.HeadSHA(b.Worktree, "origin/"+name)
		if err := gitx.Remote.Push(b.Worktree, name); err != nil {
			return err
		}
		if localSHA == "" || remoteSHA == "" || localSHA != remoteSHA {
			successf("pushed local changes to %s", styleBranch(name))
			pushedCommits = true
		}
		if b.PRNumber > 0 {
			prs[name] = b.PRNumber
			continue
		}
		base := b.Parent
		if base == "" {
			base = repo.Trunk
		}
		title := titleFlag
		if title == "" {
			title = prTitleFor(b.Worktree, base, name)
		}
		// --body short-circuits both template lookup and the editor flow
		// since the caller is providing the body wholesale. Otherwise:
		// seed from the repo's PR template (if any), then drop the user
		// into $VISUAL / $EDITOR with the title pre-filled on the first
		// line and the seed body below it (separated by a blank line) —
		// same convention as `git commit`. The user can edit either or
		// both; on save we re-parse the file using the same first-line /
		// blank-separator rule. On non-TTY shells, missing editor vars,
		// or multi-branch scope, we skip the editor and submit the seed
		// unedited. The stack-footer machinery preserves whatever sits
		// above its markers across later footer-only refreshes.
		var seed string
		if bodyFlagSet {
			seed = bodyFlag
		} else {
			template, terr := gitx.Forge.PRTemplate(b.Worktree)
			if terr != nil {
				// Don't fail the submit just because we couldn't read a
				// template; surface the error and proceed with an empty body.
				errorf("reading PR template for %s: %v", styleBranch(name), terr)
				template = ""
			}
			seed = template
			if scope == stack.ScopeBranch {
				if newTitle, newBody, ok, eerr := editPRBody(title, seed, b.Worktree, base, name); eerr != nil {
					return eerr
				} else if ok {
					title = newTitle
					seed = newBody
				}
			}
		}
		url, num, err := gitx.Forge.CreatePR(forge.CreatePROpts{
			Worktree:   b.Worktree,
			BaseBranch: base,
			HeadBranch: name,
			Title:      title,
			Body:       withUpdatedFooter(seed, name, repo.Trunk, lineage, prs),
			IsDraft:    draft,
		})
		if err != nil {
			return err
		}
		if err := state.UpdatePR(bare, name, num); err != nil {
			return err
		}
		prs[name] = num
		successf("created PR #%d: %s <- %s", num, styleBranch(base), styleBranch(name))
		hintf("view in browser @ %s", out.palette.Trunk.Render(url))
		createdPR = true
	}
	// Pass 2: refresh footers everywhere in scope. Every PR in the stack
	// shows the same tree, just with its own line bolded.
	for _, name := range ordered {
		num := prs[name]
		if num == 0 {
			continue
		}
		current, err := gitx.Forge.GetPRBody(repo.PrimaryWorktree, num)
		if err != nil {
			return err
		}
		updated := withUpdatedFooter(current, name, repo.Trunk, lineage, prs)
		if updated == current {
			continue
		}
		if err := gitx.Forge.EditPRBody(repo.PrimaryWorktree, num, updated); err != nil {
			return err
		}
	}
	if !createdPR && !pushedCommits {
		hintf("everything already up-to-date")
	}
	return nil
}

func resolveSubmitScope(cmd *cobra.Command) stack.Scope {
	if v, _ := cmd.Flags().GetBool("all"); v {
		return stack.ScopeStack
	}
	if v, _ := cmd.Flags().GetBool("upstack"); v {
		return stack.ScopeUpstack
	}
	if v, _ := cmd.Flags().GetBool("downstack"); v {
		return stack.ScopeDownstack
	}
	return stack.ScopeBranch
}

// topoFilter returns scope reordered so parents appear before children
// according to the lineage's overall topological order.
func topoFilter(l stack.Lineage, scope []string) []string {
	in := make(map[string]bool, len(scope))
	for _, n := range scope {
		in[n] = true
	}
	var out []string
	for _, n := range l.Topological() {
		if in[n] {
			out = append(out, n)
		}
	}
	return out
}

// editPRBody opens the user's editor (preferring $VISUAL, falling back
// to $EDITOR) on a tempfile pre-populated with seedTitle, an explicit
// `---` separator on its own line, seedBody, and a uniquely-tagged
// HTML comment scaffold listing the branch's unique commits. Returns
// (newTitle, newBody, true) on a successful save with non-empty body;
// (seedTitle, seedBody, false) when no editor is available, stdin
// isn't a TTY, or the editor wasn't actually invoked. An empty body
// (after stripping the scaffold) aborts the submit with an error so
// the caller can short-circuit. An empty title falls back to seedTitle
// silently — clearing only the title is almost always a slip rather
// than an intentional choice, and GitHub requires a title.
//
// Parse rule (deterministic, see splitTitleAndBody): everything above
// the first standalone `---` line is the title; everything below is
// the body. If the file has no `---` separator, the whole file is
// treated as the body and the title falls back to seedTitle — handy
// for "leave the title alone, just edit the body".
func editPRBody(seedTitle, seedBody, worktree, parent, branch string) (string, string, bool, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return seedTitle, seedBody, false, nil
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return seedTitle, seedBody, false, nil
	}
	commits, _ := gitx.Ref.UniqueCommits(worktree, parent, branch, 0)
	f, err := os.CreateTemp("", "gg-submit-*.md")
	if err != nil {
		return seedTitle, seedBody, false, fmt.Errorf("creating temp file: %w", err)
	}
	path := f.Name()
	defer os.Remove(path)
	var b strings.Builder
	// Title block, explicit `---` separator on its own line, body block.
	// The separator is unambiguous: parsing splits on the first standalone
	// `---` line, so users can't confuse "body that starts with a blank
	// line" with "no body."
	b.WriteString(seedTitle)
	b.WriteString("\n---\n")
	b.WriteString(seedBody)
	if seedBody != "" && !strings.HasSuffix(seedBody, "\n") {
		b.WriteByte('\n')
	}
	// Scaffold lives inside a uniquely-tagged HTML comment block. Markdown
	// rendering ignores HTML comments, but more importantly we strip
	// anything between the `gg-scaffold` markers before submitting — so
	// the user's own headings (`# Title`) and HTML comments survive
	// untouched. A bare `<!--` strip would risk eating user content.
	b.WriteString("\n" + scaffoldOpen + "\n")
	b.WriteString("Save and quit to submit. Everything above the first standalone `---`\n")
	b.WriteString("line is the PR title; everything below it is the body. Removing the\n")
	b.WriteString("`---` line treats the whole file as the body and keeps the seeded\n")
	b.WriteString("title. Leave the body empty (or only this scaffold) to abort.\n")
	b.WriteString("Everything inside this comment block is removed before the PR is\n")
	b.WriteString("created.\n\n")
	fmt.Fprintf(&b, "branch: %s -> %s\n", branch, parent)
	if len(commits) > 0 {
		b.WriteString("commits in this PR:\n")
		for _, c := range commits {
			fmt.Fprintf(&b, "  %s  %s\n", c.ShortSHA, c.Subject)
		}
	}
	b.WriteString(scaffoldClose + "\n")
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return seedTitle, seedBody, false, fmt.Errorf("seeding temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return seedTitle, seedBody, false, fmt.Errorf("closing temp file: %w", err)
	}
	// Run editor through `sh -c` so $VISUAL/$EDITOR values like
	// "code --wait" or "nvim -O" parse correctly without us reimplementing
	// shell quoting. stdin/stdout/stderr are passed through so terminal
	// editors take over the foreground cleanly.
	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s %q", editor, path))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return seedTitle, seedBody, false, fmt.Errorf("editor %s: %w", editor, err)
	}
	edited, err := os.ReadFile(path)
	if err != nil {
		return seedTitle, seedBody, false, fmt.Errorf("reading edited body: %w", err)
	}
	titleOut, bodyOut := splitTitleAndBody(stripScaffold(string(edited)))
	if bodyOut == "" {
		return seedTitle, seedBody, false, fmt.Errorf("body empty after edit; aborting submit for %s", branch)
	}
	if titleOut == "" {
		titleOut = seedTitle
	}
	return titleOut, bodyOut, true, nil
}

// splitTitleAndBody splits the editor input on the first standalone
// `---` line. Above the separator is the title, below is the body;
// both are trimmed of surrounding whitespace. Strict match: the line
// must be exactly `---` (trailing horizontal whitespace tolerated, but
// no leading whitespace and no other characters), so a markdown
// horizontal rule mid-body — which is also typed as `---` — only
// matters if it happens to be the first such line. Title-side rules:
//
//   - No `---` separator anywhere → returns ("", body) so the caller
//     keeps its seeded title; the user gets a "leave title alone, just
//     edit body" affordance.
//   - Title section empty (file starts with `---`) → returns ("", body)
//     and the caller falls back to its seed.
//   - Body section empty → returns (title, "") and the caller aborts.
func splitTitleAndBody(s string) (string, string) {
	lines := strings.Split(s, "\n")
	sepIdx := -1
	for i, ln := range lines {
		if isTitleBodySeparator(ln) {
			sepIdx = i
			break
		}
	}
	if sepIdx < 0 {
		return "", strings.TrimSpace(s)
	}
	title := strings.TrimSpace(strings.Join(lines[:sepIdx], "\n"))
	body := strings.TrimSpace(strings.Join(lines[sepIdx+1:], "\n"))
	return title, body
}

// isTitleBodySeparator returns true when ln is exactly three dashes,
// optionally followed by trailing horizontal whitespace. No leading
// whitespace allowed — that keeps an indented `---` (rare in markdown
// but possible in code blocks the user pasted) from accidentally
// splitting a body that hasn't actually crossed into separator land.
func isTitleBodySeparator(ln string) bool {
	return strings.TrimRight(ln, " \t") == "---"
}

const (
	scaffoldOpen  = "<!-- gg-scaffold"
	scaffoldClose = "gg-scaffold -->"
)

// stripScaffold removes the uniquely-tagged HTML comment block we insert
// when seeding the editor. Returns the body trimmed of leading/trailing
// whitespace. If no scaffold marker is found (user wiped it manually),
// returns the trimmed input unchanged.
func stripScaffold(s string) string {
	start := strings.Index(s, scaffoldOpen)
	if start < 0 {
		return strings.TrimSpace(s)
	}
	rel := strings.Index(s[start:], scaffoldClose)
	if rel < 0 {
		return strings.TrimSpace(s)
	}
	end := start + rel + len(scaffoldClose)
	return strings.TrimSpace(s[:start] + s[end:])
}

// prTitleFor picks a sensible default PR title: the subject of the newest
// commit on the branch that isn't already on the parent. Falls back to the
// branch name.
func prTitleFor(worktree, parent, branch string) string {
	if subject, err := gitx.In(worktree).
		Cmd("log", "-1", "--format=%s", parent+".."+branch).
		String(); err == nil &&
		subject != "" {
		return subject
	}
	if subject, err := gitx.In(worktree).
		Cmd("log", "-1", "--format=%s", branch).
		String(); err == nil &&
		subject != "" {
		return subject
	}
	return branch
}
