# git-good (gg)

intended to replace graphite `gt` and worktrunk `wt` for me

## reqs

- git
- gh (run `auth login` so the cli can PR for you)
- go 1.26.*

## setup

in terminal (assuming you have access to the repo)

`GOPRIVATE=github.com/ashi-labs/gg go install github.com/ashi-labs/gg/cmd/gg@latest`

verify `gg` is on path

`which gg`

view the cli help with

`gg help`

## common workflows

every command has `gg <cmd> --help` for the full story. short names in parenthesis are aliases.

### start a repo

```
gg clone <url> [name]   # clone a remote into a gg-managed repo and cd in
gg init                 # turn the current directory (empty, files, or a git clone) into a gg repo
gg link                 # re-bind a gg repo after you've moved it on disk
```

### build a stack

each branch gets its own worktree, so there's no stashing needed to switch work.

```
gg new <branch>         # (n) new branch + worktree off trunk
gg append <branch>      # (a) new branch + worktree stacked on the current one
gg add <paths>          # stage paths in the current worktree
gg commit -m "msg"      # (c) commit staged changes; -a stages tracked files first
gg amend                # amend the tip commit and restack descendants
gg restack              # (rs) rebase the whole stack onto its parents
```

quick path: create a branch and commit everything in one shot —

```
gg new fix-typo -am "fix typo"     # stage all + commit on a fresh branch off trunk; if this fails for any reason, changes are stashed
gg append add-tests -am "tests"    # ...then stack the next branch on top
```

### navigate

```
gg cd [repo[:branch[:path]]]   # interactive cross-repo picker, or jump straight to a target
gg checkout [branch]           # (co) repo-scoped picker / jump
gg up [n]                      # (upstream) move n steps toward trunk (parent)
gg down [n]                    # (downstream) move n steps toward the leaf (child)
gg first / gg last             # (1 / N) jump to the base / tip of the current stack
gg trunk                       # (0) jump to trunk
gg repos                       # pick a tracked repo and cd to its primary worktree
gg log                         # (ls) show the stack tree; -a expands commits inline
gg status                      # (stat) where am i + what needs attention
gg diff [branch]               # (d) working tree vs head; --parent vs stack parent
```

### prs & sync

```
gg submit                 # open/refresh the PR for the current branch
gg submit --all           # submit every branch in the stack
gg submit --downstack     # current branch + ancestors (--upstack for descendants)
gg submit --draft         # open PRs in draft state
gg sync                   # (s) fetch trunk, rebase the current stack, prune merged PRs
gg sync --repo            # rebase every stack in this repo (--all for every repo)
gg fold                   # squash the current branch into its parent, reparent children
gg delete -b <branch>     # (rm) delete a branch + its worktree (-r for the subtree); without -b operates as `git rm`
```

if a sync or restack pauses on a conflict, resolve it then —

```
gg continue   # resume after staging the resolution
gg abort      # bail out and reset every branch to its pre-sync sha
```

### simple demo video

TODO

## shell wrapper and completions

`gg shell install <shell>` writes the wrapper (which lets `gg` cd you
into worktrees) and completion script to standard per-user locations
and edits your rc file to source them. Shell is required and must be
one of `zsh`, `bash`, or `fish`.

```
gg shell install zsh
gg shell install bash
gg shell install fish
```

zsh / bash:

- wrapper goes to `$XDG_CONFIG_HOME/gg/wrapper.<shell>` (with the
  completion script bundled inline)
- your rc (`$ZDOTDIR/.zshrc` or `~/.bashrc`) gets a single managed
  block bounded by `# >>> gg >>>` / `# <<< gg <<<` markers

fish:

- wrapper at `$XDG_CONFIG_HOME/fish/functions/gg.fish`
- completion at `$XDG_CONFIG_HOME/fish/completions/gg.fish`
- no rc edit needed — fish autoloads from those paths

re-running `gg shell install <shell>` is idempotent: it updates the
managed block in place rather than appending. restart your shell (or
`exec zsh|bash|fish`) to pick up the wrapper.

### prefetch

pass `--prefetch` to also install a precmd hook that warms the
PR-status cache in the background so `gg log` / `gg ls` paint
instantly. on zsh a ZLE widget also fires the prefetch as soon as
you start typing `gg ls` / `gg log`.

```
gg shell install zsh --prefetch
```

### color

pass `--color=always|auto|never` to bake a color choice into every
`gg` invocation the wrapper makes (handy if stderr isn't reliably a
TTY in your shell, e.g. some multiplexers).

## claude skill / codex prompt

you can run

`gg skill [claude|codex]`

to output the claude skill / codex prompt contents to stdout. if you're ok with the output you can then save the skill / prompt via

`gg skill [claude|codex] -w`

for the claude skill it is saved into `$HOME/.claude/skills/gg/SKILL.md`

for the codex prompt it is saved into `$HOME/.codex/prompts/gg.md` and accessed via `/gg`
