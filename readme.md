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
