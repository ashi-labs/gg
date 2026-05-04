package gitx

type xCommit struct{}

func (xCommit) Create(dir, message string) error {
	return In(dir).Cmd(kCommit, dashM, message).Err()
}

func (xCommit) Tree(dir, treeSHA, message, stdin string) (string, error) {
	return In(dir).Cmd(kCommitTree, treeSHA, dashM, message).Stdin(stdin).String()
}
