package gitx

type xReset struct{}

func (xReset) Hard(dir, ref string) error {
	return In(dir).Cmd(kReset, flagHard, ref).Err()
}
