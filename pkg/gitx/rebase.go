package gitx

import (
	"os"
	"path/filepath"
)

type xRebase struct{}

func (xRebase) Abort(dir string) error {
	return In(dir).Cmd(kRebase, flagAbort).Err()
}

func (xRebase) Continue(dir string) error {
	skipEditor := "core.editor=true"
	return In(dir).Cmd(dashC, skipEditor, kRebase, flagContinue).Err()
}

func (xRebase) InProgress(dir string) (bool, error) {
	gitDir, err := gitDir(dir)
	if err != nil {
		return false, err
	}
	for _, name := range []string{kRebaseApply, kRebaseMerge} {
		if _, err := os.Stat(filepath.Join(gitDir, name)); err == nil {
			return true, nil
		}
	}
	return false, nil
}

func (xRebase) Onto(dir, newBase, oldBase string) error {
	return In(dir).Cmd(kRebase, flagOnto, newBase, oldBase).Err()
}
