package hooks

import (
	"fmt"
	"sort"
)

// Known actions that hooks can trigger.
var knownActions = map[string]bool{
	"check": true,
	"fix":   true,
}

// HookKind identifies the trigger type.
type HookKind int

const (
	KindCreate      HookKind = iota // on-create
	KindModify                      // on-modify
	KindFieldChange                 // on-field-change
)

// HookDef is the YAML-parsed hook definition.
type HookDef struct {
	Action string `yaml:"action"`
	Scope  string `yaml:"scope"`
	Field  string `yaml:"field"` // only for on-field-change
}

// Hook is a validated, ready-to-use hook.
type Hook struct {
	Name string
	Kind HookKind
	Def  HookDef
}

// hookKinds maps YAML key names to HookKind.
var hookKinds = map[string]HookKind{
	"on-create":       KindCreate,
	"on-modify":       KindModify,
	"on-field-change": KindFieldChange,
}

// ParseHooks validates hook definitions and returns parsed hooks.
// nil or empty map is valid (watch with no hooks).
func ParseHooks(defs map[string]HookDef) ([]Hook, error) {
	if len(defs) == 0 {
		return nil, nil
	}

	// Sort keys for deterministic error messages.
	keys := make([]string, 0, len(defs))
	for k := range defs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	hooks := make([]Hook, 0, len(defs))
	for _, name := range keys {
		def := defs[name]

		kind, ok := hookKinds[name]
		if !ok {
			return nil, fmt.Errorf("unknown hook %q", name)
		}

		if def.Action == "" {
			return nil, fmt.Errorf("hook %q: missing action", name)
		}
		if !knownActions[def.Action] {
			return nil, fmt.Errorf("hook %q: unknown action %q", name, def.Action)
		}

		if kind == KindFieldChange && def.Field == "" {
			return nil, fmt.Errorf("hook %q: field required for on-field-change", name)
		}

		hooks = append(hooks, Hook{Name: name, Kind: kind, Def: def})
	}
	return hooks, nil
}
