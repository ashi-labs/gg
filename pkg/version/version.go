package version

import (
	"runtime/debug"
	"strings"
	"sync"
)

// Version is the fully-formatted version string. Override at build time via
// ldflags so the binary reports its release identity:
//
//	go build -ldflags "-X github.com/ashi-labs/gg/pkg/version.Version=v0.1.0+abcdef12"
//
// The Makefile and install.sh compute this from `git describe`. When unset
// (e.g. `go run`, plain `go build`, or `go install module@version`), Build
// derives a sensible fallback from runtime/debug.ReadBuildInfo: `dev+<sha>`
// for local builds, or the module version for proxy installs. A `-dirty`
// suffix is appended whenever the working tree was modified at build time.
var Version string

var (
	once     sync.Once
	resolved string
)

func Build() string {
	once.Do(func() { resolved = build() })
	return resolved
}

func build() string {
	info, ok := debug.ReadBuildInfo()
	dirty := false
	sha := ""
	if ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) >= 8 {
					sha = s.Value[:8]
				}
			case "vcs.modified":
				if s.Value == "true" {
					dirty = true
				}
			}
		}
	}
	if Version != "" {
		return appendDirty(Version, dirty)
	}
	tag := ""
	if ok {
		tag = info.Main.Version
	}
	if tag == "" || tag == "(devel)" {
		tag = "dev"
	}
	if sha == "" {
		return appendDirty(tag, dirty)
	}
	return appendDirty(tag+"+"+sha, dirty)
}

func appendDirty(v string, dirty bool) string {
	if !dirty || strings.HasSuffix(v, "-dirty") {
		return v
	}
	return v + "-dirty"
}
