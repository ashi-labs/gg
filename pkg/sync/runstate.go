package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// RunState captures where a sync/restack was when it paused or was last active.
// It's persisted at <bare>/gg/runstate.json. A non-nil load result means a
// sync is in progress.
type RunState struct {
	Kind               string            `json:"kind"` // "sync" or "restack"
	Trunk              string            `json:"trunk"`
	InProgressBranch   string            `json:"in_progress_branch,omitempty"`
	InProgressWorktree string            `json:"in_progress_worktree,omitempty"`
	Remaining          []string          `json:"remaining,omitempty"`
	Snapshots          map[string]string `json:"snapshots"` // branch → pre-sync SHA
}

func runStatePath(bareDir string) string {
	return filepath.Join(bareDir, "gg", "runstate.json")
}

func Load(bareDir string) (*RunState, error) {
	data, err := os.ReadFile(runStatePath(bareDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rs RunState
	if err := json.Unmarshal(data, &rs); err != nil {
		return nil, err
	}
	return &rs, nil
}

func Save(bareDir string, rs *RunState) error {
	if err := os.MkdirAll(filepath.Join(bareDir, "gg"), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(runStatePath(bareDir), data, 0o644)
}

func Clear(bareDir string) error {
	err := os.Remove(runStatePath(bareDir))
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}
