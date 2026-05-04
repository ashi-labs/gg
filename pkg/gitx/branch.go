package gitx

import "fmt"

type xBranch struct{}

func (xBranch) Delete(dir, name string) error {
	return In(dir).Cmd(kBranch, dashUpperD, name).Err()
}

func (xBranch) HasLocal(dir, name string) bool {
	return In(
		dir,
	).Cmd(kShowRef, flagVerify, fmt.Sprintf("%s/%s/%s", kRefs, kHeads, name)).
		Err() ==
		nil
}

func (xBranch) Rename(dir, old, new string) error {
	return In(dir).Cmd(kBranch, dashM, old, new).Err()
}
