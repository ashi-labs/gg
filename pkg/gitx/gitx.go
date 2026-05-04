package gitx

import "github.com/ashi-labs/gg/pkg/gitx/forge"

var (
	Branch   = xBranch{}
	Clone    = xClone{}
	Commit   = xCommit{}
	Config   = xConfig{}
	Index    = xIndex{}
	Merge    = xMerge{}
	Rebase   = xRebase{}
	Ref      = xRef{}
	Remote   = xRemote{}
	Reset    = xReset{}
	Revision = xRevision{}
	Stash    = xStash{}
	Status   = xStatus{}
	Worktree = xWorktree{}
)

// set by forge.Select
var Forge forge.Forge
