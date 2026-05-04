package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// timeNow is the time source for cache stamps. Indirected so tests can
// pin it; callers see it as a regular time.Now().
var timeNow = time.Now

const (
	filename = "gg.json"
	lockname = "gg.json.lock"
)

type Repo struct {
	Origin          string `json:"origin,omitempty"`
	Trunk           string `json:"trunk,omitempty"`
	BareRepo        string `json:"bare_repo,omitempty"`
	PrimaryWorktree string `json:"primary_worktree,omitempty"`
	SyncStrategy    string `json:"sync_strategy,omitempty"`
	MergeStrategy   string `json:"merge_strategy,omitempty"`
}

// ShortName derives the repo's short name from its bare path, respecting
// whichever layout the repo was set up with.
//
//	bare layout:   bare at <parent>/myrepo.git       → "myrepo"
//	nested layout: bare at <parent>/myrepo/.bare     → "myrepo"
func (r Repo) ShortName() string {
	return filepath.Base(filepath.Dir(r.BareRepo))
}

func (r Repo) WorktreePath(branch string) string {
	container := filepath.Dir(r.BareRepo)
	return filepath.Join(container, sanitizeBranch(branch))
}

func sanitizeBranch(b string) string {
	return strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-").Replace(b)
}

type Branch struct {
	Name      string `json:"-"`
	Parent    string `json:"parent,omitempty"`
	ParentSHA string `json:"parent_sha,omitempty"`
	Worktree  string `json:"worktree,omitempty"`
	PRNumber  int    `json:"pr,omitempty"`
}

// PRStatusEntry is a cached forge PR-status snapshot. State mirrors
// forge.PRStatus.State (OPEN/MERGED/CLOSED) and IsDraft mirrors its
// IsDraft. Stored here (rather than inside Branch) so a PR-status
// prefetch can rewrite this slice without touching structural branch
// records — and so AllBranches stays purely structural.
type PRStatusEntry struct {
	PR      int    `json:"pr"`
	State   string `json:"state"`
	IsDraft bool   `json:"draft,omitempty"`
}

// PRStatusCache groups every cached PR status under a single timestamp so
// freshness is judged once per file. UpdatedAt is the unix time of the
// last write; By is keyed by branch name.
type PRStatusCache struct {
	UpdatedAt int64                    `json:"updated_at"`
	By        map[string]PRStatusEntry `json:"by,omitempty"`
}

type localState struct {
	Repo     Repo              `json:"repo"`
	Branches map[string]Branch `json:"branches,omitempty"`
	PRCache  PRStatusCache     `json:"pr_cache,omitzero"`
}

func statePath(bareDir string) string { return filepath.Join(bareDir, filename) }
func lockPath(bareDir string) string  { return filepath.Join(bareDir, lockname) }

func lock(bareDir string, exclusive bool) *os.File {
	flock, err := os.OpenFile(lockPath(bareDir), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		panic("failed to acquire flock")
	}
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(flock.Fd()), how); err != nil {
		panic(err)
	}
	return flock
}

func unlock(flock *os.File) {
	defer flock.Close()
	if err := syscall.Flock(int(flock.Fd()), syscall.LOCK_UN); err != nil {
		panic(err)
	}
}

func stateRead(bareDir string) (*localState, error) {
	data, err := os.ReadFile(statePath(bareDir))
	if errors.Is(err, os.ErrNotExist) {
		migrated, err := migrateFromGitConfig(bareDir)
		if err != nil {
			return nil, err
		}
		if migrated != nil {
			if err := stateWrite(bareDir, migrated); err != nil {
				return nil, err
			}
			return migrated, nil
		}
		return &localState{Branches: map[string]Branch{}}, nil
	}
	if err != nil {
		return nil, err
	}
	s := &localState{}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("decode %s: %w", filename, err)
	}
	if s.Branches == nil {
		s.Branches = map[string]Branch{}
	}
	return s, nil
}

func stateWrite(bareDir string, s *localState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(bareDir, filename+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), statePath(bareDir))
}

func read(bareDir string) (*localState, error) {
	flock := lock(bareDir, false)
	defer unlock(flock)
	return stateRead(bareDir)
}

func write(bareDir string, fn func(*localState)) error {
	flock := lock(bareDir, true)
	defer unlock(flock)
	s, err := stateRead(bareDir)
	if err != nil {
		return err
	}
	fn(s)
	return stateWrite(bareDir, s)
}

// --- Repo ---

func LoadRepo(bareDir string) (Repo, error) {
	s, err := read(bareDir)
	if err != nil {
		return Repo{}, err
	}
	return s.Repo, nil
}

func SaveRepo(bareDir string, r Repo) error {
	return write(bareDir, func(s *localState) { s.Repo = r })
}

// --- Branches ---

func LoadBranch(bareDir, name string) (Branch, error) {
	s, err := read(bareDir)
	if err != nil {
		return Branch{}, err
	}
	b := s.Branches[name]
	b.Name = name
	return b, nil
}

func SaveBranch(bareDir string, b Branch) error {
	return write(bareDir, func(s *localState) { s.Branches[b.Name] = b })
}

func DeleteBranch(bareDir, name string) error {
	return write(bareDir, func(s *localState) { delete(s.Branches, name) })
}

func RenameBranch(bareDir, old, new string) error {
	return write(bareDir, func(s *localState) {
		if b, ok := s.Branches[old]; ok {
			s.Branches[new] = b
			delete(s.Branches, old)
		}
	})
}

// UpdateBranch applies fn to the named branch's record and writes the result.
// If no record exists yet, fn receives a zero-valued Branch.
func UpdateBranch(bareDir, name string, fn func(*Branch)) error {
	return write(bareDir, func(s *localState) {
		b := s.Branches[name]
		fn(&b)
		s.Branches[name] = b
	})
}

func UpdateParent(bareDir, name, parent string) error {
	return UpdateBranch(bareDir, name, func(b *Branch) { b.Parent = parent })
}

func UpdateParentSHA(bareDir, name, sha string) error {
	return UpdateBranch(bareDir, name, func(b *Branch) { b.ParentSHA = sha })
}

func UpdateWorktree(bareDir, name, path string) error {
	return UpdateBranch(bareDir, name, func(b *Branch) { b.Worktree = path })
}

func UpdatePR(bareDir, name string, num int) error {
	return UpdateBranch(bareDir, name, func(b *Branch) { b.PRNumber = num })
}

// --- PR status cache ---

// LoadPRCache returns the cached PR-status snapshot for this repo. Read
// under the shared lock so concurrent writers don't catch us mid-flight.
// A missing or empty cache returns a zero-valued struct (UpdatedAt=0)
// which IsFresh below treats as stale.
func LoadPRCache(bareDir string) (PRStatusCache, error) {
	s, err := read(bareDir)
	if err != nil {
		return PRStatusCache{}, err
	}
	c := s.PRCache
	if c.By == nil {
		c.By = map[string]PRStatusEntry{}
	}
	return c, nil
}

// SavePRCache replaces the cached PR statuses wholesale. Stamps UpdatedAt
// to time.Now() so freshness comparisons go off the actual write moment.
// A nil/empty `by` map clears the cache (useful for tests / explicit
// invalidation).
func SavePRCache(bareDir string, by map[string]PRStatusEntry) error {
	now := timeNow().Unix()
	return write(bareDir, func(s *localState) {
		s.PRCache = PRStatusCache{UpdatedAt: now, By: by}
	})
}

// IsFresh reports whether the cache was written within ttl. ttl<=0 turns
// the cache off entirely (every read is treated as stale); UpdatedAt<=0
// means never written.
func (c PRStatusCache) IsFresh(ttl time.Duration) bool {
	if ttl <= 0 || c.UpdatedAt <= 0 {
		return false
	}
	return time.Since(time.Unix(c.UpdatedAt, 0)) < ttl
}

// AllBranches returns every branch that has a parent pointer recorded. A
// record missing a parent is considered malformed (partial state) and is
// omitted rather than surfaced to callers.
func AllBranches(bareDir string) ([]Branch, error) {
	s, err := read(bareDir)
	if err != nil {
		return nil, err
	}
	out := make([]Branch, 0, len(s.Branches))
	for name, b := range s.Branches {
		if b.Parent == "" {
			continue
		}
		b.Name = name
		out = append(out, b)
	}
	return out, nil
}
