package gitx

import "path/filepath"

type xRevision struct{}

func (xRevision) CommonDir(cwd string) (string, error) {
	return In(cwd).Cmd(kRevParse, flagGitCommonDir).String()
}

func (xRevision) TopLevel(cwd string) (string, error) {
	return In(cwd).Cmd(kRevParse, flagShowTopLevel).String()
}

func (xRevision) CurrentBranch(cwd string) (string, error) {
	return In(cwd).Cmd(kRevParse, flagAbbrevRef, kHead).String()
}

func (xRevision) HeadSHA(dir, ref string) (string, error) {
	return In(dir).Cmd(kRevParse, ref).String()
}

func (xRevision) IsBareRepo(dir string) bool {
	out, err := In(dir).Cmd(kRevParse, flagIsBareRepository).String()
	return err == nil && out == "true"
}

func gitDir(dir string) (string, error) {
	gitDir, err := In(dir).Cmd(kRevParse, flagGitDir).String()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	return gitDir, nil
}
