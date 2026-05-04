package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newShellCmd() *cobra.Command {
	shell := &cobra.Command{
		Use:     "shell",
		Aliases: []string{"sh"},
		Short:   "shell integration helpers.",
	}
	shell.AddCommand(newShellInstallCmd())
	return shell
}

func newShellInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <shell>",
		Short: "install the gg shell wrapper + completions and wire them into your shell rc.",
		Long: `writes the wrapper (with completion bundled inline) to a per-user
location and edits the shell rc so it gets sourced on every new
shell. idempotent — re-running this command updates the managed
block in place rather than appending duplicates.

zsh:
  wrapper:    $XDG_CONFIG_HOME/gg/wrapper.zsh (with completion inlined)
  rc:         ~/.zshrc (managed block bounded by markers)

bash:
  wrapper:    $XDG_CONFIG_HOME/gg/wrapper.bash (with completion inlined)
  rc:         ~/.bashrc (managed block bounded by markers)

fish (autoloaded by fish, so no rc edit):
  wrapper:    $XDG_CONFIG_HOME/fish/functions/gg.fish
  completion: $XDG_CONFIG_HOME/fish/completions/gg.fish

the rc-file edit is wrapped in marker comments:

  # >>> gg >>> DO NOT EDIT (managed by 'gg shell install')
  source "<wrapper-path>"
  # <<< gg <<<

to uninstall, delete the marker block from the rc and remove the
wrapper file. re-running this command after a deletion is safe.

use --color=always to bake a color choice into the wrapper (handy if
stderr isn't reliably a tty in the shell, e.g. some multiplexers).

use --prefetch to install a precmd hook that warms gg log's pr-status
cache in the background. each prompt fires a detached "gg log --prefetch"
when cwd is inside a git work tree; outside one, the hook is a no-op.
on zsh a zle widget also fires the prefetch as soon as the user starts
typing "gg ls" / "gg log", so the cache is warm by the time enter is
hit. cache entries stay fresh for pr.cache-ttl-seconds in config
(default 30s).`,
		Args: cobra.ExactArgs(1),
		RunE: runShellInstall,
	}
	cmd.Flags().
		String("color", "", "inject --color=<auto|always|never> into every gg invocation the wrapper makes")
	cmd.Flags().
		Bool("prefetch", false, "install a precmd hook that warms gg log's PR-status cache in the background")
	return cmd
}

func runShellInstall(cmd *cobra.Command, args []string) error {
	colorFlag, _ := cmd.Flags().GetString("color")
	switch colorFlag {
	case "", "auto", "always", "never":
	default:
		return fmt.Errorf("--color must be one of: auto, always, never")
	}
	prefetch, _ := cmd.Flags().GetBool("prefetch")
	colorArg := ""
	if colorFlag != "" {
		colorArg = "--color=" + colorFlag + " "
	}
	switch args[0] {
	case "zsh":
		return installBashZsh("zsh", colorArg, prefetch)
	case "bash":
		return installBashZsh("bash", colorArg, prefetch)
	case "fish":
		return installFish(colorArg, prefetch)
	default:
		return fmt.Errorf("unsupported shell %q (expected bash, zsh, or fish)", args[0])
	}
}

// rc-edit markers. These are matched literally when re-running install,
// so updates land in-place. The "DO NOT EDIT" hint is for humans glancing
// at their rc file — the contents inside are regenerated from the binary
// every install.
const (
	rcBeginMarker = "# >>> gg >>> DO NOT EDIT (managed by 'gg shell install')"
	rcEndMarker   = "# <<< gg <<<"
)

// installBashZsh writes the wrapper (with completion inlined) to
// $XDG_CONFIG_HOME/gg/wrapper.<ext> and adds a managed source
// block to the user's rc file. Wrapper and rc edit are both atomic
// (temp-file + rename) so a partial write can't leave a half-installed
// shell unable to start.
func installBashZsh(shell, colorArg string, prefetch bool) error {
	wrapper := renderBashZsh(colorArg, shell, prefetch)
	completion, err := renderCompletion(shell)
	if err != nil {
		return fmt.Errorf("rendering %s completion: %w", shell, err)
	}
	body := wrapper + "\n# --- completion (cobra-generated) ---\n" + completion
	home, _ := os.UserHomeDir()
	wrapperPath := filepath.Join(xdgConfigHome(), "gg", "wrapper."+shell)
	if err := writeFileAtomic(wrapperPath, body, 0o644); err != nil {
		return fmt.Errorf("writing wrapper to %s: %w", wrapperPath, err)
	}
	rcPath := rcFileFor(shell)
	if err := upsertRcBlock(
		rcPath,
		fmt.Sprintf("source %q\n", strings.ReplaceAll(wrapperPath, home, "$HOME")),
	); err != nil {
		return fmt.Errorf("updating %s: %w", rcPath, err)
	}
	successf("installed wrapper @ %s", strings.ReplaceAll(wrapperPath, home, "~"))
	successf("sourced @ %s", strings.ReplaceAll(rcPath, home, "~"))
	hintf("restart your shell or run: exec %s", shell)
	return nil
}

// installFish lays files into fish's autoload directories. fish loads
// functions and completions on demand from these paths, so no rc edit
// is needed — the install is complete the moment the files are in place.
func installFish(colorArg string, prefetch bool) error {
	wrapper := renderFish(colorArg, prefetch)
	completion, err := renderCompletion("fish")
	if err != nil {
		return fmt.Errorf("rendering fish completion: %w", err)
	}
	fishDir := filepath.Join(xdgConfigHome(), "fish")
	wrapperPath := filepath.Join(fishDir, "functions", "gg.fish")
	completionPath := filepath.Join(fishDir, "completions", "gg.fish")
	if err := writeFileAtomic(wrapperPath, wrapper, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", wrapperPath, err)
	}
	if err := writeFileAtomic(completionPath, completion, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", completionPath, err)
	}
	successf("installed wrapper at %s", wrapperPath)
	successf("installed completion at %s", completionPath)
	hintf("restart your shell or run: exec fish")
	return nil
}

func renderCompletion(shell string) (string, error) {
	var buf bytes.Buffer
	var err error
	switch shell {
	case "zsh":
		err = root.GenZshCompletion(&buf)
	case "bash":
		err = root.GenBashCompletion(&buf)
	case "fish":
		err = root.GenFishCompletion(&buf, true)
	default:
		return "", fmt.Errorf("no completion generator for %q", shell)
	}
	return buf.String(), err
}

func xdgConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func rcFileFor(shell string) string {
	home, _ := os.UserHomeDir()
	switch shell {
	case "zsh":
		if v := os.Getenv("ZDOTDIR"); v != "" {
			return filepath.Join(v, ".zshrc")
		}
		return filepath.Join(home, ".zshrc")
	case "bash":
		return filepath.Join(home, ".bashrc")
	default:
		return filepath.Join(home, "."+shell+"rc")
	}
}

// writeFileAtomic stages the new content in a temp file in the same
// directory and renames it over the destination, so a crash mid-write
// can't leave a truncated wrapper that breaks shell startup.
func writeFileAtomic(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// upsertRcBlock idempotently inserts (or replaces) the managed block in
// path. If the file doesn't exist it's created. If the begin marker is
// already present, everything between begin and end is replaced — so
// re-running install with a different --prefetch / --color flag updates
// the wrapper-source line without leaving stale duplicates.
func upsertRcBlock(path, body string) error {
	block := rcBeginMarker + "\n" + body + rcEndMarker + "\n"
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(existing)
	if start := strings.Index(text, rcBeginMarker); start >= 0 {
		rel := strings.Index(text[start:], rcEndMarker)
		if rel >= 0 {
			end := start + rel + len(rcEndMarker)
			if end < len(text) && text[end] == '\n' {
				end++
			}
			return writeFileAtomic(path, text[:start]+block+text[end:], 0o644)
		}
		// Begin marker present but no end marker — bail rather than
		// risk eating user content past the orphaned begin.
		return fmt.Errorf(
			"%s has a stray %q without a matching %q; remove it manually and re-run",
			path,
			rcBeginMarker,
			rcEndMarker,
		)
	}
	// Append. Pad with a leading blank line if the file already has
	// trailing content, so the block stands out visually.
	switch {
	case text == "":
		return writeFileAtomic(path, block, 0o644)
	case strings.HasSuffix(text, "\n\n"):
		return writeFileAtomic(path, text+block, 0o644)
	case strings.HasSuffix(text, "\n"):
		return writeFileAtomic(path, text+"\n"+block, 0o644)
	default:
		return writeFileAtomic(path, text+"\n\n"+block, 0o644)
	}
}

func renderBashZsh(colorArg, shell string, prefetch bool) string {
	out := strings.ReplaceAll(bashZshWrapper, "__COLOR__", colorArg)
	if !prefetch {
		return out
	}
	hook := bashZshPrefetchHook
	// zsh has its own precmd hooks (precmd_functions or `precmd () { ... }`);
	// bash users typically rely on PROMPT_COMMAND. Emit the right wiring per
	// shell so users don't have to copy-paste extra plumbing.
	switch shell {
	case "zsh":
		// On zsh we also wire a ZLE widget that fires the prefetch the
		// moment the user has typed `gg ls` / `gg log` — closes the gap
		// where the cache TTL has lapsed mid-prompt and a precmd-only
		// refresh wouldn't run again until after the next command.
		hook = strings.ReplaceAll(hook, "__REGISTER__", zshPrecmdRegister+"\n"+zshTypedPrefetchHook)
	default:
		hook = strings.ReplaceAll(hook, "__REGISTER__", bashPromptCommandRegister)
	}
	return out + "\n" + strings.ReplaceAll(hook, "__COLOR__", colorArg)
}

func renderFish(colorArg string, prefetch bool) string {
	out := strings.ReplaceAll(fishWrapper, "__COLOR__", colorArg)
	if !prefetch {
		return out
	}
	return out + "\n" + strings.ReplaceAll(fishPrefetchHook, "__COLOR__", colorArg)
}

// The wrappers below also capture the visible tmux pane (when running
// inside tmux) into a fixed path under $XDG_CACHE_HOME/gg and
// expose it to gg via GG_BACKDROP_FILE. The picker reads that file and
// renders it as top-padding behind the alt-screen UI, giving the
// picker a "popup over the scrollback" feel instead of a buffer-swap
// blank.
//
// Using a stable per-user path (rather than mktemp + cleanup) was a
// deliberate choice: some shell frameworks (z4h, prezto with command
// hooks) track tempfile paths during command execution and complain
// when they go missing mid-pipeline. A persistent path gets
// overwritten on each gg invocation and simply left in place
// otherwise — nothing to clean up, nothing to go stale.

const bashZshWrapper = `__gg_backdrop_start() {
  # Writes the current tmux pane capture (colors preserved) to a
  # stable per-user file and prints the path. Outside tmux, or on any
  # failure, prints nothing — callers treat empty output as "no
  # backdrop". No cleanup needed: the file is reused next call.
  [ -z "$TMUX" ] && return 0
  command -v tmux >/dev/null 2>&1 || return 0
  local dir="${XDG_CACHE_HOME:-$HOME/.cache}/gg"
  mkdir -p "$dir" 2>/dev/null || return 0
  local f="$dir/backdrop"
  command tmux capture-pane -e -p > "$f" 2>/dev/null || return 0
  printf '%s' "$f"
}

gg() {
  local __gg_backdrop
  __gg_backdrop="$(__gg_backdrop_start)"
  case "$1" in
    init|clone|append|a|new|n|checkout|co|upstream|up|downstream|down|first|1|last|N|trunk|0|rename|mv|fold|repos|cd|sync|s)
      local __gg_out __gg_rc
      __gg_out="$(GG_BACKDROP_FILE="$__gg_backdrop" command gg __COLOR__"$@")"
      __gg_rc=$?
      if [ "$__gg_rc" -ne 0 ]; then
        [ -n "$__gg_out" ] && printf '%s\n' "$__gg_out"
        return "$__gg_rc"
      fi
      if [ -n "$__gg_out" ] && [ -d "$__gg_out" ]; then
        cd "$__gg_out" || return
      elif [ -n "$__gg_out" ]; then
        # Captured stdout isn't a worktree path — usually --help output from
        # a cd-producing alias. Print it so the user isn't left in the dark.
        printf '%s\n' "$__gg_out"
      fi
      ;;
    *)
      GG_BACKDROP_FILE="$__gg_backdrop" command gg __COLOR__"$@"
      ;;
  esac
}
`

// bashZshPrefetchHook adds a precmd-time helper that fires `gg log
// --prefetch` in a fully detached subprocess. The trick that makes this
// invisible to the user's prompt:
//
//	( ... & ) </dev/null >/dev/null 2>&1 disown
//
//	- The outer `( )` runs the command in a subshell so the parent shell
//	  never sees the background job — no "[1] 12345" job-control noise.
//	- `& ` puts it in the background of that subshell.
//	- </dev/null >/dev/null 2>&1 detaches stdio so even if the prefetch
//	  decides to print something, it can't smudge the prompt redraw.
//	- `disown` (zsh-style; bash version uses subshell exit so the parent
//	  never tracks it) ensures the shell doesn't keep a handle on the PID.
//
// The hook bails out fast if cwd isn't inside a gg repo (cheap
// check: walk upward looking for a directory that contains either a
// .git/config with [got] section or a parent .bare/gg.json).
// The check is intentionally a heuristic — false positives just kick a
// `gg log --prefetch` that itself bails when context resolution fails.
const bashZshPrefetchHook = `__gg_prefetch() {
  # Only fire inside a git work tree to avoid useless gg startups in
  # arbitrary directories. -C cwd is implicit; redirect git stderr so the
  # check is silent in non-repo cwds.
  command git rev-parse --is-inside-work-tree >/dev/null 2>&1 || return 0
  # Detached subprocess. Subshell + redirected stdio so nothing about the
  # background fetch leaks into the user's prompt redraw.
  ( command gg __COLOR__log --prefetch >/dev/null 2>&1 & ) </dev/null >/dev/null 2>&1
}
__REGISTER__
`

const zshPrecmdRegister = `# zsh: register __gg_prefetch as a precmd hook by appending directly to
# the precmd_functions array. We avoid add-zsh-hook because z4h sources
# our wrapper via "z4h source <(...)" during early init, before its own
# fpath wiring makes add-zsh-hook autoloadable — the && short-circuits
# silently and the hook never registers. Idempotency: dedupe by name.
typeset -ga precmd_functions
(( ${precmd_functions[(I)__gg_prefetch]} )) || precmd_functions+=(__gg_prefetch)`

// zshTypedPrefetchHook fires the same detached prefetch as the precmd
// hook, but on input as soon as the buffer looks like `gg ls` / `gg log`
// (with or without args). This closes the window where the user sits
// at the prompt long enough for the cache TTL to lapse, then types a
// command — without this, no refresh would run between the previous
// precmd and the new accept-line, so the cache would already be stale
// by the time `gg log` actually executes.
//
// Debounced two ways: a per-line guard (__GG_TYPED_PREFETCH_FIRED)
// resets when a new line begins, so we only fire once per editing
// session even though zle-line-pre-redraw runs on every keystroke
// (and every cursor move). And __gg_prefetch itself is fork-only;
// concurrent fires just race a couple of detached `gh` calls — the
// state.json writes are flock+atomic-rename-safe.
//
// Install is deferred to the first precmd because z4h sources our
// wrapper via `z4h source <(...)` during its early init phase, when
// fpath / autoload are not yet wired up — calling
// add-zle-hook-widget at source-time silently no-ops there. By the
// first precmd, ZLE is fully live. We try add-zle-hook-widget (modern,
// cooperative) and fall back to wrapping any existing widget directly
// so older zsh or stripped configurations still work.
const zshTypedPrefetchHook = `__gg_typed_prefetch() {
  # Contains-match anywhere in the buffer so we still fire for things
  # like ` + "`cd repo && gg ls`" + ` or ` + "`gg ls | grep foo`" + `. A spurious match
  # on, say, ` + "`agg ls`" + ` is harmless: __gg_prefetch bails outside a git
  # work tree and is detached/silent in any case.
  emulate -L zsh
  case "$BUFFER" in
    *gg' '(ls|log)*) ;;
    *) return 0 ;;
  esac
  [[ -n "$__GG_TYPED_PREFETCH_FIRED" ]] && return 0
  __GG_TYPED_PREFETCH_FIRED=1
  __gg_prefetch
}
__gg_typed_prefetch_reset() { unset __GG_TYPED_PREFETCH_FIRED }

__gg_install_typed_prefetch() {
  emulate -L zsh
  # Run once: self-remove from precmd_functions after install.
  if autoload -Uz add-zle-hook-widget 2>/dev/null && \
     whence -w add-zle-hook-widget >/dev/null 2>&1; then
    add-zle-hook-widget line-pre-redraw __gg_typed_prefetch
    add-zle-hook-widget line-init       __gg_typed_prefetch_reset
  else
    # Fallback: wrap any existing widget by name. If nothing was
    # bound, define a fresh one. Either way, our widget calls the
    # original (if any) so we don't break framework redraw logic.
    local prev_lpr="${widgets[zle-line-pre-redraw]#user:}"
    local prev_li="${widgets[zle-line-init]#user:}"
    __gg_zle_line_pre_redraw_wrap() {
      __gg_typed_prefetch
      [[ -n "$prev_lpr" ]] && zle "$prev_lpr"
    }
    __gg_zle_line_init_wrap() {
      __gg_typed_prefetch_reset
      [[ -n "$prev_li" ]] && zle "$prev_li"
    }
    zle -N zle-line-pre-redraw __gg_zle_line_pre_redraw_wrap
    zle -N zle-line-init       __gg_zle_line_init_wrap
  fi
  precmd_functions=(${precmd_functions:#__gg_install_typed_prefetch})
}
typeset -ga precmd_functions
(( ${precmd_functions[(I)__gg_install_typed_prefetch]} )) || \
  precmd_functions+=(__gg_install_typed_prefetch)`

const bashPromptCommandRegister = `# bash: chain __gg_prefetch onto PROMPT_COMMAND. Guard against
# duplicate inclusion so re-sourcing the wrapper stays idempotent.
case ";${PROMPT_COMMAND-};" in
  *";__gg_prefetch;"*) ;;
  *) PROMPT_COMMAND="${PROMPT_COMMAND:+$PROMPT_COMMAND;}__gg_prefetch" ;;
esac`

const fishPrefetchHook = `function __gg_prefetch --on-event fish_prompt
  # Only fire inside a git work tree to avoid useless gg startups in
  # arbitrary directories.
  command git rev-parse --is-inside-work-tree >/dev/null 2>&1; or return 0
  # Detached: fish's "&" backgrounds, "disown" cuts the parent's handle.
  command gg __COLOR__log --prefetch >/dev/null 2>&1 &
  disown
end
`

const fishWrapper = `function __gg_backdrop_start
  # Writes the current tmux pane capture to a stable per-user file and
  # prints the path. Outside tmux or on failure, prints nothing.
  if not set -q TMUX; or not command -v tmux >/dev/null 2>&1
    return 0
  end
  set -l dir "$XDG_CACHE_HOME/gg"
  test -z "$XDG_CACHE_HOME"; and set dir "$HOME/.cache/gg"
  mkdir -p "$dir" 2>/dev/null
  or return 0
  set -l f "$dir/backdrop"
  command tmux capture-pane -e -p > "$f" 2>/dev/null
  or return 0
  printf '%s' "$f"
end

function gg
  set -l __gg_backdrop (__gg_backdrop_start)
  switch $argv[1]
    case init clone append a new n checkout co upstream up downstream down first 1 last N trunk 0 rename mv fold repos cd sync s
      set -l __gg_out (env GG_BACKDROP_FILE="$__gg_backdrop" command gg __COLOR__$argv)
      set -l __gg_rc $status
      if test $__gg_rc -ne 0
        test -n "$__gg_out"; and printf '%s\n' $__gg_out
        return $__gg_rc
      end
      if test -n "$__gg_out" -a -d "$__gg_out"
        cd $__gg_out
      else if test -n "$__gg_out"
        printf '%s\n' $__gg_out
      end
    case '*'
      env GG_BACKDROP_FILE="$__gg_backdrop" command gg __COLOR__$argv
  end
end
`
