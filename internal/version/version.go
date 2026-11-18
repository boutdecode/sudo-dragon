// Package version holds the build version shared by all binaries.
package version

import "runtime/debug"

// Version is set at link time:
//
//	go build -ldflags "-X github.com/kevin/ascii-adventure/internal/version.Version=v1.2.3"
//
// It stays "dev" for a plain `go build`.
var Version = "dev"

// String returns the version to display. For an unstamped build it falls back
// to the module version or VCS revision recorded by the Go toolchain.
func String() string {
	if Version != "dev" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			rev := s.Value
			if len(rev) > 12 {
				rev = rev[:12]
			}
			return "dev-" + rev
		}
	}
	return Version
}
