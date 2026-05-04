package gitx

type xMerge struct{}

func (xMerge) Squash(dir, branch string) error {
	return In(dir).Cmd(kMerge, flagSquash, branch).Err()
}
