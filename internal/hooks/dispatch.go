package hooks

import (
	"strings"
	"time"
)

// Event represents a filesystem change.
type Event struct {
	Path string    `json:"path"`
	Op   string    `json:"op"`
	Time time.Time `json:"time"`
}

// HookResult records the outcome of a hook dispatch.
type HookResult struct {
	Hook   string `json:"hook"`
	Action string `json:"action"`
	Scope  string `json:"scope"`
	Error  string `json:"error,omitempty"`
}

// Dispatch matches events to hooks and returns results.
func Dispatch(events []Event, hooks []Hook) []HookResult {
	var results []HookResult

	for _, ev := range events {
		for _, h := range hooks {
			if !matchesHook(ev, h) {
				continue
			}

			scope := renderScope(h.Def.Scope, ev.Path)
			results = append(results, HookResult{
				Hook:   h.Name,
				Action: h.Def.Action,
				Scope:  scope,
			})
		}
	}

	return results
}

func matchesHook(ev Event, h Hook) bool {
	switch h.Kind {
	case KindCreate:
		return ev.Op == "create"
	case KindModify:
		return ev.Op == "modify"
	default:
		return false
	}
}

func renderScope(tmpl, path string) string {
	if tmpl == "" {
		return path
	}
	return strings.ReplaceAll(tmpl, "{{.Path}}", path)
}
