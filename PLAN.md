# gg Feature Plan

Shared todo list for parallel agent work. Each agent should:

1. Claim a task by changing `- [ ]` to `- [~]` and appending `— claimed by <agent-id> YYYY-MM-DD`.
2. Mark complete with `- [x]` and append `— done <agent-id> YYYY-MM-DD` plus a one-line note (PR #, commit sha, or "see <file>").
3. Never start a task whose listed prerequisites are not `[x]`.
4. Respect stack ordering. Items within a stack are sequential unless marked `(parallel with N)`.
5. If blocked, flip to `- [!]` with a one-line reason.

Status legend: `[ ]` open · `[~]` in progress · `[x]` done · `[!]` blocked

---

## Parallel pool (no prerequisites — any agent, any time)

- [x] Multi-child picker for `gg down` / `gg downstream` (reuse `picker.Select`) — claimed by opus-downstream-picker 2026-04-21
- [x] Copy-ignored between worktrees: seed `.env` via clonefile/FICLONE in `gg append` (node_modules dropped — pnpm rewrites it anyway); extra paths opt-in via `seed-paths` — done opus-copy-ignored 2026-04-21 (branch `copy-ignored-append`)
- [x] `gg shell install` — write wrapper to `$fpath` + completions + reload nudge
- [x] `gg repos --sort=last-used|name|added` + config
- [-] Registry self-heal: on `resolveRepo()` mismatch, auto-insert or prompt
- [-] Glamour rendering for commit subjects in `gg log -a` — dropped 2026-05-02: glamour is a block-level renderer and the inline output looked worse than plain dim; not worth a heavy dep for marginal styling on one-line subjects.
- [ ] Pre-flight checks (independent sub-tasks, can be split):
  - [ ] Warn in `gg fold` when squash would stomp unmerged upstream commits
  - [-] Warn in `gg rename` when PR body footer needs re-submitting — dropped 2026-05-03: superseded by 3.2 (rename now auto-refreshes footers, so there's nothing for the user to action).
  - [x] Confirm in `gg delete` if branch has unpushed commits — done 2026-05-03 (delete.go hasUnpushedCommits enriches the prune confirm with a "will be lost" warning when local tip differs from origin/<name>).

---

## Stack 1 — `gg log` upgrades

Shared rendering pipeline; downstream items rework if order breaks.

- [x] 1.1 Performance caching layer: memoize per-invocation `AllBranches` — done opus-log-cache 2026-04-21 (process-scoped `internal/cache` + gitcfg integration on branch `log-cache`)
- [-] 1.2 `gg log --json` — skipped 2026-04-21: no current user demand; internal consumers want a shared Go model, not an external flag. Revisit if scripting use cases surface.
- [x] 1.3 `gg log <branch>` — focus log to one stack *(prereq: 1.1)* — done opus-log-focus 2026-04-21 (branch `log-focus` stacked on `log-cache`)
- [x] 1.4 CI/PR status column (●/✓/✗ for open/merged/failing) via `gh pr view` in parallel with short TTL cache *(prereq: 1.1)* — done opus-log-pr-status 2026-04-21 (see pkg/cli/log.go + log_progress.go)
- [x] 1.5 `gg ls -a` full commit-log timeline rendering per branch *(prereq: 1.1, 1.4)* — done opus-log-commits 2026-05-02 (pkg/cli/log.go fetchCommitLines + pkg/gitx/ref.go UniqueCommits; verified live)
- [-] 1.6 `gg status` — dropped 2026-05-02: `gg ls` / `gg ls -a` already cover the "where am I + stack health" use case; a separate status surface would duplicate without adding much.

---

## Stack 2 — Forge breadth

- [x] 2.1 Extract `internal/forge` interface; make current `gh` calls the GitHub impl — done 2026-05-02 (pkg/gitx/forge/forge.go Forge interface + github.go impl + Select() router by remote URL)
- [ ] 2.2 Fake-`gh` shell mock + `gg submit` e2e test (captures `pr-create/view/edit` to state dir) *(prereq: 2.1)*
- [ ] 2.3 GitLab (`glab`) forge implementation *(prereq: 2.1, parallel with 2.4, 2.5)*
- [ ] 2.4 Gitea (`tea`) forge implementation *(prereq: 2.1, parallel with 2.3, 2.5)*
- [ ] 2.5 Bitbucket forge implementation *(prereq: 2.1, parallel with 2.3, 2.4)*

---

## Stack 3 — Sync & PR lifecycle

- [x] 3.1 Merge-cleanup on `gg sync`: detect merged PRs on GitHub, reparent children onto trunk, delete local branch + worktree — done 2026-05-02 (pkg/cli/sync.go pruneMergedBranches; per-branch confirm under ttySuspender)
- [x] 3.2 PR body footer auto-refresh, wired into `gg rename`, `gg fold`, `gg delete --recursive`, and merge-cleanup *(prereq: 3.1)* — done 2026-05-02 (rename.go/fold.go/delete.go each call refreshOpenPRFooters() best-effort after their mutation; gg delete also refreshes for non-recursive single-branch case since children get reparented either way)
- [x] 3.3 PR title/body authoring on `gg submit`: `--title`, `--body`, `--editor` ($EDITOR with template + commit-list scaffold), and respect forge PR templates (`.github/PULL_REQUEST_TEMPLATE.md` and friends). Land in slices: (a) Forge.PRTemplate() + auto-inject above stack footer; (b) `--title`/`--body` flags; (c) `--editor` flow.

---

## Stack 4 — Config & hooks

- [ ] 4.1 `gg config` subcommand — manage `~/.config/got/config.toml` (editor, merge strategy, color, etc.)
- [x] 4.2 Config validation at startup — clear "run `gg init` or `gg link`" message when `got.trunk` missing — done 2026-05-03 (context.go resolveCtx error now lists both setup paths). Note: prereq 4.1 turned out unnecessary — the change was a one-line error-message swap, no config subcommand required.
- [ ] 4.3 Hooks system in committed `.config/got.toml`: pre/post-append, pre/post-sync, pre/post-submit, on-branch-switch *(prereq: 4.1)*

---

## Stack 5 — Safety net (land after Stacks 2–3 settle to avoid re-diffing)

- [ ] 5.1 Undo subsystem using reverse-opcode pattern (each mutating command records its inverse; `gg undo` rolls back last op) *(prereq: Stack 2 complete, Stack 3 complete)*

---

## Stack 6 — Release

- [x] 6.1 Tag-aware version string (`v0.1.0+sha` at a tag, `dev+sha` between tags) — done opus-release 2026-05-02 (pkg/version/version.go + install.sh)
- [x] 6.2 Release automation: tag-triggered GH Actions workflow producing stamped darwin/linux amd64+arm64 tarballs + checksums + auto release *(prereq: 6.1)* — done opus-release 2026-05-02 (.github/workflows/release.yml)

---

## Stack 7 — Internals refactor (after Stack 1 stabilizes)

- [ ] 7.1 Swap shell-outs for go-git on **read paths only** (refs, config parsing — not writes) *(prereq: Stack 1 complete)*

---

## Coordination notes

- Stack 1 caching layer (1.1) and Stack 7 (7.1) both touch read paths — do 1.1 first, then 7.1 can build on it.
- Stack 3 footer refresh (3.2) depends on forge calls; if Stack 2.1 (forge interface) is already done, route through the interface instead of `gh` directly.
- Pre-flight checks in the parallel pool pair naturally with Stack 5 undo but don't block it.
- If an agent finishes a task and the next item in its stack is blocked, drop to the parallel pool rather than starting an out-of-order stack item.
