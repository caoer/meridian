// Package version provides build-time version info injected via ldflags,
// with a runtime/debug.ReadBuildInfo fallback for `go install` builds.
package version

import "runtime/debug"

// Set via ldflags: -ldflags "-X github.com/caoer/meridian/internal/version.version=..."
var version = "dev"

// Info returns the version string. Priority:
//  1. ldflags-injected value (non-"dev")
//  2. VCS revision from runtime/debug.ReadBuildInfo
//  3. "dev" (unset)
func Info() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 8 {
				return "dev+" + s.Value[:8]
			}
		}
	}
	return version
}
