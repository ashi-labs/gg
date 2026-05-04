package gitx

import (
	"fmt"
	"strings"
)

type xRemote struct{}

func (xRemote) DeleteBranch(dir, branch string) error {
	return In(dir).Cmd(kPush, kOrigin, flagDelete, branch).Err()
}

func (xRemote) Exists(dir, name string) bool {
	return In(dir).Cmd(kRemote, kGetURL, name).Err() == nil
}

func (xRemote) FetchOrigin(dir string) error {
	return In(dir).Cmd(kFetch, kOrigin).Err()
}

func (xRemote) HasBranch(dir, branch string) bool {
	return In(
		dir,
	).Cmd(kShowRef, flagVerify, flagQuiet, fmt.Sprintf("%s/%s/%s/%s", kRefs, kRemotes, kOrigin, branch)).
		Err() ==
		nil
}

func (xRemote) Push(dir, branch string) error {
	// Use an explicit lease keyed to the remote's actual SHA rather than the
	// local remote-tracking ref, which can be stale when a branch with this
	// name was previously deleted on origin (e.g., after a PR merge).
	remoteSHA := remoteHeadSHA(dir, branch)
	if remoteSHA == "" {
		return In(dir).Cmd(kPush, flagSetUpstream, kOrigin, branch).Err()
	}
	lease := fmt.Sprintf("%s=%s:%s", flagForceWithLease, branch, remoteSHA)
	return In(dir).Cmd(kPush, lease, flagSetUpstream, kOrigin, branch).Err()
}

func remoteHeadSHA(dir, branch string) string {
	out, err := In(dir).Cmd(kLsRemote, kOrigin, kRefs+"/"+kHeads+"/"+branch).String()
	if err != nil || out == "" {
		return ""
	}
	// Output: "<sha>\trefs/heads/<branch>"
	if i := strings.IndexAny(out, " \t"); i > 0 {
		return out[:i]
	}
	return ""
}

func (xRemote) SetHeadAuto(dir, name string) error {
	return In(dir).Cmd(kRemote, kSetHead, name, flagAuto).Err()
}

func (xRemote) SetUpstreamConfig(dir, branch string) error {
	if err := In(
		dir,
	).Cmd(kConfig, fmt.Sprintf("%s.%s.%s", kBranch, branch, kRemote), kOrigin).
		Err(); err != nil {
		return err
	}
	return In(
		dir,
	).Cmd(kConfig, fmt.Sprintf("%s.%s.%s", kBranch, branch, kMerge), fmt.Sprintf("%s/%s/%s", kRefs, kHeads, branch)).
		Err()
}

func (xRemote) URL(dir, name string) string {
	out, _ := In(dir).Cmd(kRemote, kGetURL, name).String()
	return out
}
