package state

import (
	"bufio"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// migrateFromGitConfig reads legacy state from the bare repo's git config
// (keys under `gg.*`). Returns nil if no legacy state is present so
// callers treat this as "fresh repo, start empty". Called once per repo on
// the first state read; the subsequent writeState persists the JSON form.
func migrateFromGitConfig(bareDir string) (*localState, error) {
	configFile := filepath.Join(bareDir, "config")
	cmd := exec.Command("git", "config", "--file", configFile, "--get-regexp", `^gg\.`)
	cmd.Dir = bareDir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil // no legacy keys present
		}
		return nil, err
	}
	s := &localState{Branches: map[string]Branch{}}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), " ")
		if !found {
			continue
		}
		switch key {
		case "gg.trunk":
			s.Repo.Trunk = value
		case "gg.bare-repo":
			s.Repo.BareRepo = value
		case "gg.primary-worktree":
			s.Repo.PrimaryWorktree = value
		case "gg.sync-strategy":
			s.Repo.SyncStrategy = value
		case "gg.merge-strategy":
			s.Repo.MergeStrategy = value
		default:
			const prefix = "gg.branch."
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			rest := key[len(prefix):]
			// rest = <branch>.<field>; branch names may contain dots so split on LAST dot.
			dot := strings.LastIndexByte(rest, '.')
			if dot < 0 {
				continue
			}
			name, field := rest[:dot], rest[dot+1:]
			b := s.Branches[name]
			switch field {
			case "parent":
				b.Parent = value
			case "parent-sha":
				b.ParentSHA = value
			case "worktree":
				b.Worktree = value
			case "pr":
				if n, err := strconv.Atoi(value); err == nil {
					b.PRNumber = n
				}
			}
			s.Branches[name] = b
		}
	}
	// If nothing matched any known key, treat as empty.
	if s.Repo == (Repo{}) && len(s.Branches) == 0 {
		return nil, nil
	}
	return s, nil
}
