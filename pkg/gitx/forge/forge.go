package forge

import "strings"

type CreatePROpts struct {
	Worktree   string
	BaseBranch string
	HeadBranch string
	Title      string
	Body       string
	IsDraft    bool
}

// PRStatus is the forge's view of a PR. State is the lifecycle bucket
// (OPEN/CLOSED/MERGED); IsDraft is only meaningful when State is OPEN.
type PRStatus struct {
	State   string
	IsDraft bool
}

type Forge interface {
	CreatePR(opts CreatePROpts) (string, int, error)
	GetPRBody(worktree string, num int) (string, error)
	EditPRBody(worktree string, num int, body string) error
	GetPRBaseBranch(worktree string, num int) (string, error)
	GetPRStatus(worktree string, num int) (PRStatus, error)
	PRTemplate(worktree string) (string, error)
}

func Select(remoteURL string) Forge {
	if remoteURL == "" {
		return nil
	}
	switch {
	case strings.Contains(remoteURL, "github.com"):
		return GitHub
	}
	return nil
}
