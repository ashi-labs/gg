package gitx

type xIndex struct{}

func (xIndex) AddAll(dir string) error {
	return In(dir).Cmd(kAdd, dashUpperA).Err()
}
