package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// Config is the serialized form of a user's gg config. Top-level
// fields are reserved for cross-cutting metadata; everything else
// lives under a section so the on-disk shape stays scannable as the
// surface grows.
type Config struct {
	// Path where the config was found; unset if not found.
	Path string `toml:"-" yaml:"-"`

	UI     UIConfig     `toml:"ui"     yaml:"ui"`
	Prompt PromptConfig `toml:"prompt" yaml:"prompt"`
	Log    LogConfig    `toml:"log"    yaml:"log"`
	PR     PRConfig     `toml:"pr"     yaml:"pr"`
	Seed   SeedConfig   `toml:"seed"   yaml:"seed"`
	Repos  ReposConfig  `toml:"repos"  yaml:"repos"`
	Status StatusConfig `toml:"status" yaml:"status"`
}

// UIConfig groups appearance knobs that affect every command's output.
type UIConfig struct {
	// Which theme to use for colorized output. `inherit` passes through
	// integer terminal color codes.
	Theme string `toml:"theme" yaml:"theme"`
	// Force colorized output even when stdout isn't a TTY.
	ForceColor bool `toml:"force-color" yaml:"force-color"`
	// Which glyph set to use: unicode, nerd.
	Glyphs string `toml:"glyphs" yaml:"glyphs"`
}

// PromptConfig — settings for interactive pickers (huh).
type PromptConfig struct {
	// Where the prompt is located in your terminal: top, bottom.
	Location string `toml:"location" yaml:"location"`
}

// LogConfig — `gg log` / `gg ls` rendering knobs.
type LogConfig struct {
	// Which columns to include.
	Columns []string `toml:"columns" yaml:"columns"`
	// Per-branch cap when `gg ls -a` expands commits inline. 0 = no cap.
	CommitsPerBranch int `toml:"commits-per-branch" yaml:"commits-per-branch"`
}

// PRConfig — PR-related caching/forge behavior.
type PRConfig struct {
	// How long a cached PR-status entry stays fresh, in seconds. 0 = the
	// cache is bypassed (every `gg log` does a live fetch). Used in tandem
	// with the precmd-hook prefetch from `gg shell init --prefetch`.
	CacheTTLSeconds int `toml:"cache-ttl-seconds" yaml:"cache-ttl-seconds"`
}

// SeedConfig — files copied from a parent worktree into freshly-created
// child worktrees (.env, etc.).
type SeedConfig struct {
	Paths []string `toml:"paths" yaml:"paths"`
}

// ReposConfig — `gg repos` defaults.
type ReposConfig struct {
	// Default order. Overridable per-invocation via --sort. One of:
	// last-used (most-recently cd'd into first), name (alphabetical),
	// added (oldest registry entry first).
	SortBy string `toml:"sort-by" yaml:"sort-by"`
}

// StatusConfig — `gg status` rendering knobs.
type StatusConfig struct {
	// Cap on how many ancestors/descendants `gg status` renders in its
	// lineage line. 0 = no cap (show every branch in lineage).
	MaxStackDepth int `toml:"max-stack-depth" yaml:"max-stack-depth"`
	// AheadBehindAgainst selects which comparison(s) the ahead/behind
	// block prints. One of: parent, trunk, both.
	AheadBehindAgainst string `toml:"ahead-behind-against" yaml:"ahead-behind-against"`
}

const (
	StatusAheadBehindParent         = "parent"
	StatusAheadBehindTrunk          = "trunk"
	StatusAheadBehindBoth           = "both"
	DefaultStatusAheadBehindAgainst = StatusAheadBehindParent
)

// DefaultStatusMaxStackDepth caps the lineage chain `gg status`
// renders. Most stacks fit comfortably in this many; deeper ones get
// truncated with an `…+N` marker on either end.
const DefaultStatusMaxStackDepth = 5

// ValidStatusAheadBehindAgainst is the accepted set, used by the merge
// validator and exposed so error messages can list options.
var ValidStatusAheadBehindAgainst = []string{
	StatusAheadBehindParent,
	StatusAheadBehindTrunk,
	StatusAheadBehindBoth,
}

const (
	TokyoNightTheme = "tokyo-night"
	InheritTheme    = "inherit"
	DefaultTheme    = InheritTheme
)

const (
	PromptLocationTop     = "top"
	PromptLocationBottom  = "bottom"
	DefaultPromptLocation = PromptLocationTop
)

const (
	GlyphsUnicode = "unicode"
	GlyphsNerd    = "nerd"
	DefaultGlyphs = GlyphsUnicode
)

const (
	LogColumnAheadBehind = "ahead-behind"
	LogColumnAge         = "age"
	LogColumnSubject     = "subject"
	LogColumnPR          = "pr"
	LogColumnStatus      = "status"
)

func DefaultLogColumns() []string {
	return []string{LogColumnAheadBehind, LogColumnAge}
}

const DefaultLogCommitsPerBranch = 5

// DefaultPRCacheTTLSeconds — 30s strikes a balance: long enough for the
// precmd prefetch to stay relevant across a few prompt cycles, short
// enough that PR transitions (open → merged) show up within a normal
// review back-and-forth without forcing --no-cache.
const DefaultPRCacheTTLSeconds = 30

const SeedPathDotEnv = ".env"

func DefaultSeedPaths() []string {
	return []string{".env"}
}

const (
	ReposSortLastUsed = "last-used"
	ReposSortName     = "name"
	ReposSortAdded    = "added"
	DefaultReposSort  = ReposSortLastUsed
)

// ValidReposSort is the set of accepted repos-sort values, used by the
// CLI flag validator and exposed so error messages can list options.
var ValidReposSort = []string{ReposSortLastUsed, ReposSortName, ReposSortAdded}

const (
	ExtensionToml = "toml"
	ExtensionYaml = "yaml"
	ExtensionYml  = "yml"
)

// BaseDir returns the directory gg's config lives in:
// $XDG_CONFIG_HOME/gg, falling back to $HOME/.config/gg.
func BaseDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "gg")
}

// DefaultPath returns the canonical on-disk path for `gg config -w`
// to write the default config to. TOML wins among the supported
// extensions; users who prefer YAML can rename after writing.
func DefaultPath() string {
	return filepath.Join(BaseDir(), "config.toml")
}

func readConfig(path string) []byte {
	data, err := os.ReadFile(path)
	if err == nil {
		return data
	}
	return nil
}

// Default returns the full Config struct with every field populated to
// its built-in default. Used by `gg config` to render or write the
// canonical baseline.
func Default() Config { return defaultConfig() }

func defaultConfig() Config {
	return Config{
		UI: UIConfig{
			Theme:  DefaultTheme,
			Glyphs: DefaultGlyphs,
		},
		Prompt: PromptConfig{
			Location: DefaultPromptLocation,
		},
		Log: LogConfig{
			Columns:          DefaultLogColumns(),
			CommitsPerBranch: DefaultLogCommitsPerBranch,
		},
		PR: PRConfig{
			CacheTTLSeconds: DefaultPRCacheTTLSeconds,
		},
		Seed: SeedConfig{
			Paths: DefaultSeedPaths(),
		},
		Repos: ReposConfig{
			SortBy: DefaultReposSort,
		},
		Status: StatusConfig{
			AheadBehindAgainst: DefaultStatusAheadBehindAgainst,
			MaxStackDepth:      DefaultStatusMaxStackDepth,
		},
	}
}

func Load() Config {
	cfg := defaultConfig()
	base := BaseDir()
	var extension, cfgPath string
	var cfgData []byte
	for _, extension = range []string{"toml", "yaml", "yml"} {
		file := fmt.Sprintf("config.%s", extension)
		cfgPath = filepath.Join(base, file)
		if cfgData = readConfig(cfgPath); cfgData != nil {
			break
		}
	}
	if cfgData == nil {
		return cfg
	}
	var parsed Config
	switch extension {
	case ExtensionToml:
		if _, err := toml.Decode(string(cfgData), &parsed); err != nil {
			return cfg
		}
	case ExtensionYaml, ExtensionYml:
		if err := yaml.Unmarshal(cfgData, &parsed); err != nil {
			return cfg
		}
	}
	cfg.Path = cfgPath
	mergeUI(&cfg.UI, parsed.UI)
	mergePrompt(&cfg.Prompt, parsed.Prompt)
	mergeLog(&cfg.Log, parsed.Log)
	mergePR(&cfg.PR, parsed.PR)
	mergeSeed(&cfg.Seed, parsed.Seed)
	mergeRepos(&cfg.Repos, parsed.Repos)
	mergeStatus(&cfg.Status, parsed.Status)
	return cfg
}

// Merge helpers below: each treats the zero value of a field as "user
// didn't set this, keep the default." Nuances:
//   - List fields distinguish nil (default) from explicit empty slice
//     (opt-out — keep the user's empty list).
//   - Negative numerics opt out of caps where the consumer treats <=0
//     as "no limit" (Log.CommitsPerBranch, PR.CacheTTLSeconds).
//   - Enum-like strings only override when the parsed value passes the
//     enum's validator (Repos.SortBy, Status.AheadBehindAgainst).
//
// ForceColor is a bool; explicit `false` is indistinguishable from
// "unset" with the default decoder. Default is false anyway, so the
// only case that matters (`true`) is preserved when the user sets it.

func mergeUI(dst *UIConfig, src UIConfig) {
	if src.Theme != "" {
		dst.Theme = src.Theme
	}
	if src.Glyphs != "" {
		dst.Glyphs = src.Glyphs
	}
	if src.ForceColor {
		dst.ForceColor = src.ForceColor
	}
}

func mergePrompt(dst *PromptConfig, src PromptConfig) {
	if src.Location != "" {
		dst.Location = src.Location
	}
}

func mergeLog(dst *LogConfig, src LogConfig) {
	if src.Columns != nil {
		dst.Columns = src.Columns
	}
	if src.CommitsPerBranch != 0 {
		dst.CommitsPerBranch = src.CommitsPerBranch
	}
}

func mergePR(dst *PRConfig, src PRConfig) {
	if src.CacheTTLSeconds != 0 {
		dst.CacheTTLSeconds = src.CacheTTLSeconds
	}
}

func mergeSeed(dst *SeedConfig, src SeedConfig) {
	if src.Paths != nil {
		dst.Paths = src.Paths
	}
}

func mergeRepos(dst *ReposConfig, src ReposConfig) {
	if slices.Contains(ValidReposSort, src.SortBy) {
		dst.SortBy = src.SortBy
	}
}

func mergeStatus(dst *StatusConfig, src StatusConfig) {
	if src.MaxStackDepth != 0 {
		dst.MaxStackDepth = src.MaxStackDepth
	}
	if slices.Contains(ValidStatusAheadBehindAgainst, src.AheadBehindAgainst) {
		dst.AheadBehindAgainst = src.AheadBehindAgainst
	}
}
