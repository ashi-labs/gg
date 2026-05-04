package gitx

type xClone struct{}

func (xClone) Bare(dir, url, dest string) error {
	return In(dir).Cmd(kClone, flagBare, url, dest).Err()
}
