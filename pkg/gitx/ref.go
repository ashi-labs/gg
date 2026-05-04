package gitx

import (
	"fmt"
	"strings"
)

type xRef struct{}

type CommitInfo struct {
	ShortSHA      string // abbreviated commit SHA (only set by UniqueCommits)
	Subject       string // first line of the commit message
	UnixTimestamp int64  // epoch seconds of the commit
}

func (xRef) AheadBehind(dir, left, right string) (int, int, error) {
	out, err := In(
		dir,
	).Cmd(kRevList, flagLeftRight, flagCount, fmt.Sprintf("%s...%s", left, right)).
		String()
	if err != nil {
		return 0, 0, err
	}
	var ahead, behind int
	if _, err := fmt.Sscanf(out, "%d\t%d", &ahead, &behind); err != nil {
		return 0, 0, err
	}
	return ahead, behind, nil
}

func (xRef) DefaultBranch(dir string) (string, error) {
	if out, err := In(
		dir,
	).Cmd(kSymbolicRef, flagShort, fmt.Sprintf("%s/%s/%s/%s", kRefs, kRemotes, kOrigin, kHead)).
		String(); err == nil &&
		out != "" {
		if _, rest, ok := strings.Cut(out, "/"); ok {
			return rest, nil
		}
		return out, nil
	}
	return In(dir).Cmd(kSymbolicRef, flagShort, kHead).String()
}

func (xRef) LatestCommits(dir string, names []string) (map[string]CommitInfo, error) {
	if len(names) == 0 {
		return nil, nil
	}
	cmd := In(dir).Cmd(kForEachRef, fmt.Sprintf("%s=%s", flagFormat, latestCommitFormat))
	for _, n := range names {
		cmd.Args(fmt.Sprintf("%s/%s/%s", kRefs, kHeads, n))
	}
	out, err := cmd.String()
	if err != nil {
		return nil, err
	}
	result := map[string]CommitInfo{}
	for line := range strings.SplitSeq(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, nullByte, 3)
		if len(parts) != 3 {
			continue
		}
		var unix int64
		if _, err := fmt.Sscanf(parts[2], "%d", &unix); err != nil {
			continue
		}
		result[parts[0]] = CommitInfo{Subject: parts[1], UnixTimestamp: unix}
	}
	return result, nil
}

// UniqueCommits returns commits that exist on `branch` but not on `parent`,
// in `git log` order (newest first). limit caps the result; pass 0 for no
// cap. Each entry carries ShortSHA, Subject, and UnixTimestamp. Empty result
// (zero unique commits) is returned without error.
func (xRef) UniqueCommits(dir, parent, branch string, limit int) ([]CommitInfo, error) {
	cmd := In(dir).Cmd(kLog, fmt.Sprintf("%s=%s", flagFormat, uniqueCommitFormat))
	if limit > 0 {
		cmd.Args(fmt.Sprintf("%s=%d", flagMaxCount, limit))
	}
	cmd.Args(fmt.Sprintf("%s..%s", parent, branch))
	out, err := cmd.String()
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var commits []CommitInfo
	for line := range strings.SplitSeq(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, nullByte, 3)
		if len(parts) != 3 {
			continue
		}
		var unix int64
		if _, err := fmt.Sscanf(parts[2], "%d", &unix); err != nil {
			continue
		}
		commits = append(commits, CommitInfo{
			ShortSHA:      parts[0],
			Subject:       parts[1],
			UnixTimestamp: unix,
		})
	}
	return commits, nil
}

// CountCommits returns the number of commits on `branch` not on `parent`.
// Cheap (`rev-list --count`) compared to re-listing; the caller uses it to
// compute the exact remainder when a `gg ls -a` listing is capped.
func (xRef) CountCommits(dir, parent, branch string) (int, error) {
	out, err := In(dir).Cmd(kRevList, flagCount, fmt.Sprintf("%s..%s", parent, branch)).String()
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscanf(out, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}

func (xRef) Update(dir, ref, sha string) error {
	return In(dir).Cmd(kUpdateRef, ref, sha).Err()
}

func (xRef) SetHead(dir, branch string) error {
	return In(dir).Cmd(kSymbolicRef, kHead, fmt.Sprintf("%s/%s/%s", kRefs, kHeads, branch)).Err()
}
