package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// Config is the serialized form of a user's gg config
type Config struct {
	// Path where the config was found; unset if not found
	Path string
	// Which theme to use for colorized output: tokyo-night, inherit
	// `inherit` just passes through integer terminal color codes
	Theme string `toml:"theme" yaml:"theme"`
	// Which columns to include in the `gg log/ls` command
	// TODO: authoritative list of columns in comment
	LogColumns []string `toml:"log-columns" yaml:"log-columns"`
	// Per-branch cap when `gg ls -a` expands commits inline. 0 = no cap.
	LogCommitsPerBranch int `toml:"log-commits-per-branch" yaml:"log-commits-per-branch"`
	// How long a cached PR-status entry stays fresh, in seconds. 0 = the
	// cache is bypassed (every `gg log` does a live fetch). Used in tandem
	// with the precmd-hook prefetch from `gg shell init --prefetch`.
	PRCacheTTLSeconds int `toml:"pr-cache-ttl-seconds" yaml:"pr-cache-ttl-seconds"`
	// Whether to force colorized output: yes, no, auto
	ForceColor bool `toml:"force-color" yaml:"force-color"`
	// Where the prompt is located in your terminal: top, bottom
	PromptLocation string `toml:"prompt-location" yaml:"prompt-location"`
	// Which glyph set to use: unicode, nerd
	Glyphs string `toml:"glyphs"`
	// Set of paths to copy from the parent worktree into children
	SeedPaths []string `toml:"seed-paths"`
	// How `gg repos` orders entries by default. Overridable per-invocation
	// via --sort. One of: last-used (most-recently cd'd into first),
	// name (alphabetical), added (oldest registry entry first).
	SortReposBy string `toml:"sort-repos-by" yaml:"sort-repos-by"`
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

func configBaseDir() string {
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

func readConfig(path string) []byte {
	data, err := os.ReadFile(path)
	if err == nil {
		return data
	}
	return nil
}

func Load() Config {
	cfg := Config{
		Theme:               DefaultTheme,
		LogColumns:          DefaultLogColumns(),
		LogCommitsPerBranch: DefaultLogCommitsPerBranch,
		PRCacheTTLSeconds:   DefaultPRCacheTTLSeconds,
		PromptLocation:      DefaultPromptLocation,
		Glyphs:              DefaultGlyphs,
		SeedPaths:           DefaultSeedPaths(),
		SortReposBy:         DefaultReposSort,
	}
	base := configBaseDir()
	var extension string
	var cfgPath string
	var cfgData []byte
	for _, extension = range []string{"toml", "yaml", "yml"} {
		file := fmt.Sprintf("config.%s", extension)
		cfgPath = filepath.Join(base, file)
		if cfgData = readConfig(cfgPath); cfgData != nil {
			break
		}
	}
	if cfgData != nil {
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
		if parsed.Theme != "" {
			cfg.Theme = parsed.Theme
		}
		// explicit empty list (not nil) opts out of additional log columns
		if parsed.LogColumns != nil {
			cfg.LogColumns = parsed.LogColumns
		}
		// Unset (zero value) keeps the default. Negative values opt out of
		// capping entirely — fetchCommitLines treats anything <=0 as "no cap."
		if parsed.LogCommitsPerBranch != 0 {
			cfg.LogCommitsPerBranch = parsed.LogCommitsPerBranch
		}
		// PR cache TTL: unset (0) keeps the default. To explicitly disable
		// the cache, set a negative value (gg log treats <=0 as "always
		// fresh-fetch").
		if parsed.PRCacheTTLSeconds != 0 {
			cfg.PRCacheTTLSeconds = parsed.PRCacheTTLSeconds
		}
		if parsed.PromptLocation != "" {
			cfg.PromptLocation = parsed.PromptLocation
		}
		if parsed.Glyphs != "" {
			cfg.Glyphs = parsed.Glyphs
		}
		// explicit empty list (not nil) opts out of sedding
		if parsed.SeedPaths != nil {
			cfg.SeedPaths = parsed.SeedPaths
		}
		// Empty string keeps the default. Unknown values silently fall
		// back to the default — repos.go re-validates explicit --sort
		// flags so a bad CLI value still errors loudly.
		if slices.Contains(ValidReposSort, parsed.SortReposBy) {
			cfg.SortReposBy = parsed.SortReposBy
		}
	}
	return cfg
}
