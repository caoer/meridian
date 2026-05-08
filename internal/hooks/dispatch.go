package hooks

import (
	"reflect"
	"strings"
	"sync"
	"time"
)

// Event represents a filesystem change (mirrored from watch package to avoid circular dep).
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

// Dispatcher matches events to hooks and tracks frontmatter for field-change detection.
type Dispatcher struct {
	mu    sync.Mutex
	cache map[string]map[string]any // path → frontmatter
}

// NewDispatcher creates a Dispatcher with empty frontmatter cache.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{cache: make(map[string]map[string]any)}
}

// CacheFrontmatter stores frontmatter for a path (used as "old" state for field-change detection).
func (d *Dispatcher) CacheFrontmatter(path string, fm map[string]any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache[path] = fm
}

// Dispatch matches events to hooks. Does not handle on-field-change (no new frontmatter provided).
func (d *Dispatcher) Dispatch(events []Event, hooks []Hook) []HookResult {
	return d.DispatchWithFrontmatter(events, hooks, nil)
}

// DispatchWithFrontmatter matches events to hooks, using newFM for field-change detection.
func (d *Dispatcher) DispatchWithFrontmatter(events []Event, hooks []Hook, newFM map[string]map[string]any) []HookResult {
	var results []HookResult

	for _, ev := range events {
		for _, h := range hooks {
			if !matchesHook(ev, h) {
				continue
			}

			// Field-change: compare old vs new
			if h.Kind == KindFieldChange {
				if !d.fieldChanged(ev.Path, h.Def.Field, newFM) {
					continue
				}
			}

			scope := renderScope(h.Def.Scope, ev.Path)
			results = append(results, HookResult{
				Hook:   h.Name,
				Action: h.Def.Action,
				Scope:  scope,
			})
		}
	}

	d.mu.Lock()
	// Evict cache for deleted files
	for _, ev := range events {
		if ev.Op == "delete" {
			delete(d.cache, ev.Path)
		}
	}
	// Update cache with new frontmatter
	for path, fm := range newFM {
		d.cache[path] = fm
	}
	d.mu.Unlock()

	return results
}

func matchesHook(ev Event, h Hook) bool {
	switch h.Kind {
	case KindCreate:
		return ev.Op == "create"
	case KindModify:
		return ev.Op == "modify"
	case KindFieldChange:
		return ev.Op == "modify"
	default:
		return false
	}
}

func (d *Dispatcher) fieldChanged(path, field string, newFM map[string]map[string]any) bool {
	d.mu.Lock()
	oldFM := d.cache[path]
	d.mu.Unlock()

	if oldFM == nil {
		return false // no previous state — treat as no change
	}
	if newFM == nil {
		return false
	}
	current, ok := newFM[path]
	if !ok {
		return false
	}

	return !reflect.DeepEqual(oldFM[field], current[field])
}

func renderScope(tmpl, path string) string {
	if tmpl == "" {
		return path
	}
	return strings.ReplaceAll(tmpl, "{{.Path}}", path)
}
