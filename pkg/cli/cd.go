package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ashi-labs/gg/pkg/gitx"
	"github.com/ashi-labs/gg/pkg/registry"
	"github.com/ashi-labs/gg/pkg/stack"
	"github.com/ashi-labs/gg/pkg/state"
	"github.com/ashi-labs/gg/pkg/tui/picker"
	"github.com/ashi-labs/gg/pkg/tui/style"
	"github.com/charmbracelet/lipgloss/tree"
	"github.com/spf13/cobra"
)

func newCdCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cd [repo[:branch[:path]]]",
		Short: "Jump to any tracked branch (and optionally a path within it).",
		Long: `cd accepts a colon-separated target in up to three tiers:

  gg cd                         flat picker across every repo's branches
  gg cd <repo>                  repo-scoped: picker, or auto-skip if 1 branch
  gg cd <repo>:<branch>         jump straight to that branch's worktree
  gg cd <repo>:<branch>:<path>  jump inside a branch, at a subpath

When <path> points to a file, cd resolves to the file's parent directory
so the shell's ` + "`cd`" + ` accepts it.

Completion is tier-aware: ` + "`r<Tab>`" + ` completes repo names, ` + "`r:b<Tab>`" + `
completes branches in repo, ` + "`r:b:p<Tab>`" + ` completes paths within that
branch's worktree (skipping .bare and .git).`,
		Args:              cobra.RangeArgs(0, 1),
		RunE:              runCd,
		ValidArgsFunction: completeCd,
	}
}

func runCd(cmd *cobra.Command, args []string) error {
	entries, err := registry.Load()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no repos tracked yet (run `gg clone <url>` or `gg init`)")
	}

	if len(args) == 1 {
		return runCdArg(entries, args[0])
	}
	return runCdPicker(entries)
}

// parseCdArg splits `repo:branch:path` into components. Empty segments are
// returned as empty strings. A bare `repo` with no colons returns only
// repoName populated.
func parseCdArg(s string) (repoName, branch, path string) {
	parts := strings.SplitN(s, ":", 3)
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	return parts[0], parts[1], parts[2]
}

// runCdArg handles the arg-supplied form: `gg cd repo`, `gg cd repo:branch`,
// or `gg cd repo:branch:path`.
func runCdArg(entries []registry.Entry, raw string) error {
	repoName, branch, path := parseCdArg(raw)
	if repoName == "" {
		return fmt.Errorf("repo name required (syntax: repo[:branch[:path]])")
	}

	var repo *registry.Entry
	for i := range entries {
		if entries[i].Name == repoName {
			repo = &entries[i]
			break
		}
	}
	if repo == nil {
		return fmt.Errorf("no tracked repo named %q", repoName)
	}
	if s := repo.Validate(); s != registry.StatusOK {
		return fmt.Errorf(
			"%s: %s (run `gg link` from its new location, or `gg cleanup` to drop the entry)",
			repo.Name, s,
		)
	}
	// No branch segment → fall through to repo-scoped picker (preserves
	// the single-branch auto-skip that `gg cd <repo>` used to have).
	if branch == "" {
		return runCdPicker([]registry.Entry{*repo})
	}
	worktree, err := resolveWorktree(*repo, branch)
	if err != nil {
		return err
	}
	if path == "" {
		stdout(worktree)
		return nil
	}
	target := filepath.Join(worktree, path)
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("%s: %w", target, err)
	}
	// Files can't be `cd`-ed into — land on the parent directory so the
	// shell wrapper's `cd` doesn't bounce. Matches what you'd get from
	// `cd $(dirname path/to/file)`.
	if !info.IsDir() {
		target = filepath.Dir(target)
	}
	stdout(target)
	return nil
}

// resolveWorktree returns the worktree path for branch within repo. Trunk
// resolves to the primary worktree; other branches come from the config's
// per-branch worktree pointer. Missing branch → human-friendly error.
func resolveWorktree(e registry.Entry, branch string) (string, error) {
	if branch == e.Trunk {
		return e.PrimaryWorktree, nil
	}
	branches, err := state.AllBranches(e.Bare)
	if err != nil {
		return "", err
	}
	for _, b := range branches {
		if b.Name == branch {
			if b.Worktree == "" {
				return "", fmt.Errorf("no worktree recorded for %s:%s", e.Name, branch)
			}
			return b.Worktree, nil
		}
	}
	return "", fmt.Errorf("no tracked branch %q in %s", branch, e.Name)
}

// runCdPicker shows the fuzzy-filter picker across `entries` and
// emits the chosen path. Single-entry scopes apply `gg cd <repo>`'s
// single-branch auto-skip via resolveCdTarget(..., 1); multi-repo
// calls always show the picker when there's more than one
// destination.
func runCdPicker(entries []registry.Entry) error {
	path, err := resolveCdTarget(entries, 1)
	if err != nil {
		return err
	}
	if path != "" {
		stdout(path)
	}
	return nil
}

// resolveCdTarget picks a cd destination from the given registry
// scope. Callers emit the returned path (or a command-specific
// variation — `gg co`, for instance, wraps this in a "no change"
// detector). Returns "" with nil error when the user cancels the
// picker.
//
// `autoSkipUpTo` applies only to single-entry scopes. It's the
// maximum number of real (non-trunk, non-repo) picks at or below
// which we skip the UI and return the obvious target:
//
//   - 0 picks  → the repo's primary worktree (trunk is sole
//     destination)
//   - ≤ autoSkipUpTo picks → the first pick
//   - more → show the picker
//
// `gg cd <repo>` passes autoSkipUpTo=1 (keeps the graphite-style
// "every level has one choice" shortcut). `gg co` passes 0 (trunk
// should never get hidden by auto-skipping to the one tracked
// branch).
func resolveCdTarget(entries []registry.Entry, autoSkipUpTo int) (string, error) {
	items, err := buildCdItems(entries)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", fmt.Errorf(
			"no branches found across tracked repos (run `gg repos` to inspect state)",
		)
	}
	if len(items) == 1 {
		return items[0].Path, nil
	}
	if len(entries) == 1 {
		var picks []picker.Item
		for _, it := range items {
			if !it.IsTrunk && !it.IsRepo {
				picks = append(picks, it)
			}
		}
		if len(picks) == 0 {
			return entries[0].PrimaryWorktree, nil
		}
		if len(picks) <= autoSkipUpTo {
			return picks[0].Path, nil
		}
	}
	chosen, ok, err := picker.SelectFiltered(items, "select a branch")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil // user cancelled — not an error
	}
	return chosen.Path, nil
}

// buildCdItems flattens every valid repo's stack tree into a single
// picker-ready slice, preserving lineage visually. Each repo becomes a
// top-level node (green); its trunk is the next level (yellow); branches
// form a stack tree under the trunk, matching `gg ls`. During fuzzy
// filter the tree glyphs fall away and items render flat with their
// respective accent colors — see the picker delegate.
//
// AllBranches calls run in parallel because loading N repos serially is
// the main latency on large registries.
func buildCdItems(entries []registry.Entry) ([]picker.Item, error) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	type loaded struct {
		entry    registry.Entry
		branches []state.Branch
		err      error
	}
	results := make([]loaded, len(entries))
	var wg sync.WaitGroup
	for i, e := range entries {
		if e.Validate() != registry.StatusOK {
			results[i] = loaded{entry: e}
			continue
		}
		wg.Add(1)
		go func(i int, e registry.Entry) {
			defer wg.Done()
			bs, err := state.AllBranches(e.Bare)
			results[i] = loaded{entry: e, branches: bs, err: err}
		}(i, e)
	}
	wg.Wait()

	var rows []cdRow
	var baseLines, hoverLines []string
	pal := style.Stderr

	// Each repo renders as its own free-standing tree (not nested under a
	// global invisible root) so repo rows sit flush-left instead of being
	// indented under a phantom parent. We concatenate the per-repo
	// renderings to build the final block.
	for _, r := range results {
		if r.err != nil {
			return nil, fmt.Errorf("loading %s: %w", r.entry.Name, r.err)
		}
		if r.entry.Validate() != registry.StatusOK {
			continue
		}

		repoBase := tree.Root(cdRepoLabel(r.entry.Name, pal, false))
		repoHover := tree.Root(cdRepoLabel(r.entry.Name, pal, true))
		rows = append(rows, cdRow{
			Branch:     r.entry.Bare + ":" + r.entry.Trunk,
			Path:       r.entry.PrimaryWorktree,
			FilterName: r.entry.Name,
			IsRepo:     true,
		})

		// Trunk as a child of the repo node.
		trunkBase := tree.Root(trunkLabel(r.entry.Trunk, "", pal, false))
		trunkHover := tree.Root(trunkLabel(r.entry.Trunk, "", pal, true))
		repoBase.Child(trunkBase)
		repoHover.Child(trunkHover)
		rows = append(rows, cdRow{
			Branch:     r.entry.Bare + ":" + r.entry.Trunk,
			Path:       r.entry.PrimaryWorktree,
			FilterName: r.entry.Name + "/" + r.entry.Trunk,
			IsTrunk:    true,
		})

		// Stack roots under the trunk, recursively.
		l := stack.Build(r.entry.Trunk, r.branches)
		stackRoots := l.Roots()
		sort.Strings(stackRoots)
		for _, sr := range stackRoots {
			bc, hc := buildCdBranchNode(r.entry, l, sr, pal, &rows)
			trunkBase.Child(bc)
			trunkHover.Child(hc)
		}

		repoBase.EnumeratorStyle(pal.Dim)
		// Hover tree scaffolding in Dirty so a hovered row's tree
		// lines match its recoloured marker + name. Matches the
		// convention in stacktree.go.
		repoHover.EnumeratorStyle(pal.Dirty)
		baseLines = append(
			baseLines,
			strings.Split(strings.TrimRight(repoBase.String(), "\n"), "\n")...)
		hoverLines = append(
			hoverLines,
			strings.Split(strings.TrimRight(repoHover.String(), "\n"), "\n")...)
	}

	// Pre-order DFS guarantees 1:1 alignment, but guard against future
	// shape changes.
	if len(rows) != len(baseLines) || len(rows) != len(hoverLines) {
		return nil, fmt.Errorf(
			"internal: tree render row mismatch (rows=%d, base=%d, hover=%d)",
			len(rows), len(baseLines), len(hoverLines),
		)
	}
	items := make([]picker.Item, 0, len(rows))
	for i, r := range rows {
		items = append(items, picker.Item{
			Branch:     r.Branch,
			Path:       r.Path,
			Label:      baseLines[i],
			HoverLabel: hoverLines[i],
			FilterName: r.FilterName,
			IsRepo:     r.IsRepo,
			IsTrunk:    r.IsTrunk,
		})
	}
	preselectCurrent(items)
	return items, nil
}

// preselectCurrent marks the picker item matching the user's cwd
// repo:branch as Current so the picker's cursor lands on it at
// startup. Best-effort: silent no-op when cwd isn't inside a tracked
// repo, or when the repo's entry isn't in this item set (e.g.
// `gg cd <some-other-repo>`). The repo-anchor row is always skipped
// since it isn't a selectable destination for the cursor.
func preselectCurrent(items []picker.Item) {
	branch, err := gitx.Revision.CurrentBranch(cwd)
	if err != nil || branch == "" {
		return
	}
	target := bare + ":" + branch
	for i := range items {
		if items[i].IsRepo {
			continue
		}
		if items[i].Branch == target {
			items[i].Current = true
			return
		}
	}
}

// cdRow is the structured half of each tree-rendered line. The tree's
// pre-order DFS traversal matches the order these are appended, so the
// rendered lines zip 1:1 with these rows.
type cdRow struct {
	Branch     string
	Path       string
	FilterName string
	IsRepo     bool
	IsTrunk    bool
}

// cdRepoLabel renders a repo anchor — the top row of its sub-tree.
// Green filled-dot + repo name in Repo style. Hover variant re-colors to
// Dirty with the name underlined, matching the conventions set by the
// stack tree in stacktree.go.
func cdRepoLabel(name string, pal style.Palette, hover bool) string {
	marker := style.Glyphs.Repo + " "
	if hover {
		return pal.Dirty.Render(marker) + pal.Dirty.Underline(true).Render(name)
	}
	return pal.Repo.Render(marker + name)
}

// buildCdBranchNode recurses a single repo's lineage starting at `name`,
// appending rows to the slice and returning the matched (base, hover)
// tree nodes.
func buildCdBranchNode(
	e registry.Entry,
	l stack.Lineage,
	name string,
	pal style.Palette,
	rows *[]cdRow,
) (*tree.Tree, *tree.Tree) {
	base := tree.Root(branchLabel(name, "", pal, false))
	hover := tree.Root(branchLabel(name, "", pal, true))
	b := l.ByName[name]
	*rows = append(*rows, cdRow{
		Branch:     e.Bare + ":" + name,
		Path:       b.Worktree,
		FilterName: e.Name + "/" + name,
	})
	kids := l.Children(name)
	sort.Strings(kids)
	for _, c := range kids {
		bc, hc := buildCdBranchNode(e, l, c, pal, rows)
		base.Child(bc)
		hover.Child(hc)
	}
	return base, hover
}

// ─────────────────────────────────────────────────────────────────────────
// Completion (three-tier: repo / branch / path)
// ─────────────────────────────────────────────────────────────────────────

func completeCd(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	switch strings.Count(toComplete, ":") {
	case 0:
		return completeCdRepos()
	case 1:
		parts := strings.SplitN(toComplete, ":", 2)
		return completeCdBranches(parts[0])
	case 2:
		repo, rest, _ := strings.Cut(toComplete, ":")
		branch, path, _ := strings.Cut(rest, ":")
		return completeCdPaths(repo, branch, path)
	default:
		panic("unreachable")
	}
}

// Completion output contract:
//   - Emit the bare token the user would have typed (no trailing ":" or
//     "/"). The user adds the separator themselves to move to the next
//     tier. Auto-appending the separator felt paternalistic and broke
//     the "complete what I typed" expectation.
//   - Always combine NoFileComp + NoSpace — the shell shouldn't append
//     its own filesystem completions or a trailing space after a
//     matched token, since the user may want to continue typing.

func completeCdRepos() ([]string, cobra.ShellCompDirective) {
	entries, err := registry.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		desc := e.Trunk
		if e.Origin != "" {
			desc += " · " + e.Origin
		}
		out = append(out, e.Name+"\t"+desc)
	}
	return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

func completeCdBranches(repoName string) ([]string, cobra.ShellCompDirective) {
	entries, err := registry.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var repo *registry.Entry
	for i := range entries {
		if entries[i].Name == repoName {
			repo = &entries[i]
			break
		}
	}
	if repo == nil || repo.Validate() != registry.StatusOK {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	branches, err := state.AllBranches(repo.Bare)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	out := []string{repoName + ":" + repo.Trunk + "\ttrunk"}
	for _, b := range branches {
		out = append(out, repoName+":"+b.Name+"\toff "+b.Parent)
	}
	return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

func completeCdPaths(repoName, branch, pathPart string) ([]string, cobra.ShellCompDirective) {
	entries, err := registry.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var repo *registry.Entry
	for i := range entries {
		if entries[i].Name == repoName {
			repo = &entries[i]
			break
		}
	}
	if repo == nil || repo.Validate() != registry.StatusOK {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	worktree, err := resolveWorktree(*repo, branch)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	dir, namePrefix := filepath.Split(pathPart)
	absDir := filepath.Join(worktree, dir)
	dirents, err := os.ReadDir(absDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var out []string
	for _, ent := range dirents {
		name := ent.Name()
		// Always hide gg's plumbing; hide other dotfiles unless the user
		// is explicitly typing a dot prefix.
		if name == ".bare" || name == ".git" {
			continue
		}
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(namePrefix, ".") {
			continue
		}
		if !strings.HasPrefix(name, namePrefix) {
			continue
		}
		out = append(out, repoName+":"+branch+":"+dir+name)
	}
	return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}
