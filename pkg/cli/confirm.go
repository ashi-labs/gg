package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
)

// confirmYesNo prompts via huh for an interactive yes/no decision. Title is
// the question shown to the user (e.g. "Delete 3 branches?"). Returns the
// user's choice on a TTY; without a TTY returns an error pointing the user
// at --yes — silently assuming-yes for destructive actions on a non-TTY
// would be the wrong default.
//
// Shared by every confirm-before-destroy call site (cleanup, delete, fold,
// sync prune) so the prompt UI / theme / non-TTY behavior stays consistent.
func confirmYesNo(title string) (bool, error) {
	if !isatty.IsTerminal(os.Stderr.Fd()) {
		return false, fmt.Errorf(
			"no TTY for confirmation; rerun with --yes to skip the prompt",
		)
	}
	var confirmed bool
	prompt := huh.NewConfirm()
	if title != "" {
		prompt = prompt.Title(title)
	}
	prompt = prompt.
		Affirmative("yes").
		Negative("no").
		Value(&confirmed)
	form := huh.NewForm(huh.NewGroup(prompt)).WithTheme(huhTheme())
	if err := form.Run(); err != nil {
		return false, err
	}
	return confirmed, nil
}
