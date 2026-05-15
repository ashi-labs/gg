package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "print or install gg usage instructions for a coding agent.",
		Long: `gg skill <agent> prints a markdown file teaching a coding agent
how to use gg's stacked-PR + worktree-per-branch workflow. with -w/--write
it lands at the agent's canonical user-level location instead.

supported agents:
  claude  Claude Code skill at ~/.claude/skills/gg/SKILL.md
  codex   Codex CLI prompt at ~/.codex/prompts/gg.md (becomes /gg)

each agent prints by default so you can pipe the body somewhere else,
diff against an existing install, or paste into a project-level skill.
-w writes the body to the canonical path; if a file already exists
there you'll be prompted to overwrite.`,
	}
	cmd.AddCommand(newSkillAgentCmd("claude", claudeSkillPath, claudeSkillBody))
	cmd.AddCommand(newSkillAgentCmd("codex", codexSkillPath, codexSkillBody))
	return cmd
}

func newSkillAgentCmd(agent string, pathFn func() string, body string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   agent,
		Short: fmt.Sprintf("print or write the gg skill for %s.", agent),
		Long: fmt.Sprintf(`prints the gg skill for %s to stdout. with -w/--write,
writes it to %s.

the skill teaches the agent gg's worktree-per-branch model, when to
reach for new vs append, how cd / submit / sync compose, and the
stdout-path-then-cd contract the shell wrapper relies on.`, agent, pathFn()),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSkillAgent(cmd, body, pathFn())
		},
	}
	cmd.Flags().BoolP("write", "w", false, "write the skill to the canonical path")
	return cmd
}

func runSkillAgent(cmd *cobra.Command, body, path string) error {
	write, _ := cmd.Flags().GetBool("write")
	if !write {
		stdout(body)
		hintf("add -w to install this skill @ %s", out.palette.Dim.Render(path))
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		ok, err := confirmYesNo("skill already exists. overwrite it?")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("cancelled")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create skill dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	successf("skill installed @ %s", out.palette.Dim.Render(path))
	return nil
}

// claudeSkillPath returns the canonical install path for Claude Code's
// user-level skills directory. Claude Code reads from ~/.claude/skills/<name>/SKILL.md
// regardless of XDG_CONFIG_HOME, so we don't honor it here.
func claudeSkillPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "skills", "gg", "SKILL.md")
}

// codexSkillPath returns the canonical install path for a Codex CLI
// custom prompt. Files in ~/.codex/prompts/<name>.md become /<name>
// slash commands inside Codex.
func codexSkillPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "prompts", "gg.md")
}

const skillBody = `gg ("git-good") is a stacked-PR + worktree-per-branch git workflow tool.
each tracked branch lives in its own git worktree under a shared bare
repo. the cli composes git, gh, and a small state file to keep stacks
restackable and PRs in sync.

## the worktree contract

` + "`gg cd`" + `, ` + "`gg new`" + `, ` + "`gg append`" + `, ` + "`gg checkout`" + `, ` + "`gg sync`" + ` and a few
others print the destination worktree path on stdout. the user's shell
wrapper (` + "`gg shell install`" + `) cd's into that path. when invoking gg from
a tool that does not source the wrapper, capture stdout and chdir to it
yourself if you want subsequent commands to run inside the new worktree.
log/status/diff messages go to stderr so capturing stdout is safe.

## when to start a new branch

- ` + "`gg new <name>`" + ` — net-new work. branches off main into a fresh
  trunk worktree. use this for unrelated tasks.
- ` + "`gg append <name>`" + ` — same line of thought, separate PR. branches
  off the current worktree, stacking the new branch on top. use this when
  the new work depends on the current branch but should ship as its own
  reviewable PR.
- ` + "`gg fold`" + ` — collapse the current branch into its parent. use when
  what you thought needed two PRs really only needs one.

prefer ` + "`new`" + ` for unrelated work and ` + "`append`" + ` to continue a stack;
do not commit unrelated changes onto an existing trunk.

## day-to-day commands

commits:
  gg add [paths...]      stage (interactive picker if no args)
  gg commit -m "msg"     commit staged changes
  gg amend               amend the tip commit (no message edit by default)
  gg stash / gg restore  shelve / restore working changes
  gg reset / gg revert   undo staged or committed changes

stack shape:
  gg new <name>          new trunk off main
  gg append <name>       new branch off current
  gg rename <new>        rename current branch (and its worktree)
  gg delete <branch>     delete branch + worktree
  gg reparent <parent>   move current branch (and descendants) onto a new parent
  gg track / untrack     bring an existing branch under gg / let it go
  gg restack             rebase the stack after main has moved

navigation:
  gg cd <branch>         jump to a worktree (tab-completes branch names)
  gg checkout <branch>   alias for cd
  gg upstream / down     move one branch up / down the stack
  gg first / last        jump to the bottom / top of the stack
  gg trunk               jump to the trunk of the current stack
  gg repos               pick across all gg-managed repos

state:
  gg status              working-tree status for the current worktree
  gg log (alias: ls)     paint the stack with PR status
  gg diff                diff helpers (staged, unstaged, vs trunk)

remotes:
  gg fetch               git fetch + refresh PR status cache
  gg sync                pull main, restack, prune merged branches
  gg submit              push the stack and open/update PRs via gh

conflicts:
  gg continue            resume after resolving a conflict
  gg abort               abort the in-progress restack / sync

admin:
  gg init / clone        create or adopt a gg-managed repo
  gg link                link an existing bare repo into gg
  gg cleanup             prune dangling worktrees / state
  gg config              print or write the default config
  gg shell install       install the shell wrapper + completions
  gg version

## conventions

- branch names: lowercase-kebab. gg uses them as both branch and
  worktree directory names.
- never run ` + "`git checkout`" + ` to move between gg branches — it leaves
  the worktree state inconsistent. use ` + "`gg cd`" + `.
- ` + "`gg sync`" + ` is the safe way to pull main; it restacks every branch
  that needs it and prompts before pruning merged work.
- prefer ` + "`gg submit`" + ` over raw ` + "`git push`" + ` so PR metadata stays linked
  in the state file.
- color output is a TTY thing; pass ` + "`--color=always`" + ` or ` + "`--color=never`" + `
  to force when piping or capturing.

## when something goes wrong

- conflicts during ` + "`sync`" + ` / ` + "`restack`" + `: resolve in the worktree git
  put you in, then ` + "`gg continue`" + `. ` + "`gg abort`" + ` rolls back to where you
  started.
- "not in a gg repo" errors: you're outside a worktree gg knows about.
  ` + "`gg repos`" + ` lists known repos; cd into one of them.
- a worktree directory that gg lost track of: ` + "`gg cleanup`" + ` reconciles
  state with on-disk worktrees.

run ` + "`gg <cmd> --help`" + ` for the long-form description of any command.`

// claudeSkillBody is the Claude Code skill: YAML frontmatter (read by
// the skill loader) followed by the shared body. The description field
// is what Claude sees when deciding whether to autoload the skill, so
// it's written to surface on stack/worktree/gg keywords.
const claudeSkillBody = `---
name: gg
description: Use gg ("git-good") for stacked-PR and worktree-per-branch git workflows. Trigger when the user mentions stacks, trunks, worktrees, or invokes commands prefixed with gg (gg new, gg append, gg cd, gg submit, gg sync, gg log/ls, etc.), or when working inside a repo that uses a .bare worktree layout managed by gg.
---

` + skillBody + `
`

// codexSkillBody is the Codex CLI prompt body. Codex reads the file
// directly when its slash command fires, so no frontmatter — just the
// body. A short header reminds the agent why it was invoked.
const codexSkillBody = `# gg — stacked-PR + worktree-per-branch workflow

` + skillBody + `
`
