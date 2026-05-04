package gitx

type xStash struct{}

func (xStash) PushStaged(dir, message string) error {
	return In(dir).Cmd(kStash, kPush, flagStaged, dashM, message).Err()
}

func (xStash) Pop(dir string) error {
	return In(dir).Cmd(kStash, kPop).Err()
}
