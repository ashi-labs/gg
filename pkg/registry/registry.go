// Package registry tracks every repo that gg has created or adopted. The
// store lives at $XDG_STATE_HOME/gg/repos.toml (falling back to
// ~/.local/state/gg/repos.toml) — state, not config, because the paths are
// host-local and should not sync across machines via dotfile repos.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
)

// Entry is one tracked repository.
type Entry struct {
	Name            string    `toml:"name"`
	Bare            string    `toml:"bare"`
	PrimaryWorktree string    `toml:"primary_worktree"`
	Trunk           string    `toml:"trunk"`
	Origin          string    `toml:"origin,omitempty"`
	AddedAt         time.Time `toml:"added_at"`
	LastUsedAt      time.Time `toml:"last_used_at,omitempty"`
}

// Status describes the on-disk validity of an entry.
type Status int

const (
	StatusOK Status = iota
	StatusBareMissing
	StatusPrimaryMissing
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusBareMissing:
		return "bare missing"
	case StatusPrimaryMissing:
		return "primary worktree missing"
	default:
		return "unknown"
	}
}

// Validate stats the entry's paths and returns its current on-disk status.
func (e Entry) Validate() Status {
	if _, err := os.Stat(e.Bare); err != nil {
		return StatusBareMissing
	}
	if _, err := os.Stat(e.PrimaryWorktree); err != nil {
		return StatusPrimaryMissing
	}
	return StatusOK
}

type registryFile struct {
	Repos []Entry `toml:"repos"`
}

// Path returns the on-disk location of the registry.
func Path() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "gg", "repos.toml")
}

// Load reads the registry. Returns an empty slice if the file doesn't exist.
func Load() ([]Entry, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rf registryFile
	if _, err := toml.Decode(string(data), &rf); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", Path(), err)
	}
	return rf.Repos, nil
}

// Upsert inserts or replaces an entry keyed by Bare. LastUsedAt is set from
// e.LastUsedAt if provided, else left as-is when replacing, else set to now.
func Upsert(e Entry) error {
	entries, err := Load()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if e.AddedAt.IsZero() {
		e.AddedAt = now
	}
	if e.LastUsedAt.IsZero() {
		e.LastUsedAt = now
	}
	found := false
	for i := range entries {
		if entries[i].Bare == e.Bare {
			e.AddedAt = entries[i].AddedAt
			entries[i] = e
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, e)
	}
	return save(entries)
}

// Remove drops the entry whose Bare matches. Returns nil if no such entry.
func Remove(bare string) error {
	entries, err := Load()
	if err != nil {
		return err
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if entry.Bare == bare {
			continue
		}
		filtered = append(filtered, entry)
	}
	return save(filtered)
}

// Touch updates LastUsedAt for the entry matching bare. Silently no-ops if
// the entry isn't registered — touch is a best-effort signal.
func Touch(bare string) error {
	entries, err := Load()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	changed := false
	for i := range entries {
		if entries[i].Bare == bare {
			entries[i].LastUsedAt = now
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	return save(entries)
}

// Get returns the entry for a given bare path, or (Entry{}, false) if none.
func Get(bare string) (Entry, bool, error) {
	entries, err := Load()
	if err != nil {
		return Entry{}, false, err
	}
	for _, e := range entries {
		if e.Bare == bare {
			return e, true, nil
		}
	}
	return Entry{}, false, nil
}

func save(entries []Entry) error {
	// Sort by last-used descending so the most-recent repos land at the top
	// of the file — easier to read when inspecting manually.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].LastUsedAt.After(entries[j].LastUsedAt)
	})
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(f).Encode(registryFile{Repos: entries}); err != nil {
		defer f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
