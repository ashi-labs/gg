package gitx

type xStatus struct{}

func (xStatus) HasStaged(dir string) (bool, error) {
	code, err := In(dir).Cmd(kDiff, flagCached, flagQuiet).ExitCode()
	if err != nil {
		return false, err
	}
	return code == 1, nil
}

func (xStatus) IsDirty(dir string) (bool, error) {
	out, err := In(dir).Cmd(kStatus, flagPorcelain).String()
	if err != nil {
		return false, err
	}
	return out != "", nil
}
