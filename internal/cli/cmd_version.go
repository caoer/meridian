package cli

import (
	"runtime"
	"runtime/debug"

	"github.com/caoer/meridian/internal/version"
)

// buildRevision extracts the vcs revision (+ dirty marker) embedded by the Go
// toolchain, so `md version` distinguishes stale binaries — a build without
// vcs info (e.g. go build outside a checkout) reports "unknown".
func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "", ""
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if rev == "" {
		return "unknown"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	return rev + dirty
}

func NewVersionHandler() Handler {
	return func(req *Request) *Response {
		return &Response{
			Version: ResponseVersion,
			Data: map[string]string{
				"meridian": version.Info(),
				"build":    buildRevision(),
				"go":       runtime.Version(),
				"platform": runtime.GOOS + "/" + runtime.GOARCH,
			},
		}
	}
}
