package rules

import "strings"

// Rule is a single lint rule loaded from YAML.
type Rule struct {
	ID       string         // filename stem, globally unique
	Check    string         // registered check type name
	Message  string         // Go template string
	Severity Severity       // error/warn/info/off
	On       OnFilter       // parsed file filter
	Params   map[string]any // check-specific parameters
	Required bool           // if true, an unregistered check is a tool failure (exit 2), not a skipped warning
}

// OnFilter holds parsed components of a rule's `on` field.
// Path globs: ANY must match (OR).
// Tags: ALL must match (AND).
// Excludes: ANY removes (OR).
type OnFilter struct {
	Paths    []string  // plain path globs
	Tags     []string  // tag values (without # prefix)
	Excludes []Exclude // negated path or tag
}

// Exclude is a single negation — either a path glob or a tag.
type Exclude struct {
	Path string // non-empty if path exclude
	Tag  string // non-empty if tag exclude
}

// ApplyConfigParams merges config-level rule params into loaded rules.
// For each rule whose ID matches a key in configParams, the config values
// are shallow-merged into the rule's Params (config wins on conflict).
// Returns a new slice — does not mutate originals.
func ApplyConfigParams(loaded []Rule, configParams map[string]map[string]any) []Rule {
	if len(configParams) == 0 {
		return loaded
	}
	result := make([]Rule, len(loaded))
	for i, r := range loaded {
		cp, ok := configParams[r.ID]
		if !ok || len(cp) == 0 {
			result[i] = r
			continue
		}
		// Copy rule to avoid mutating original.
		merged := make(map[string]any, len(r.Params)+len(cp))
		for k, v := range r.Params {
			merged[k] = v
		}
		for k, v := range cp {
			merged[k] = v // config wins
		}
		nr := r
		nr.Params = merged
		result[i] = nr
	}
	return result
}

// ParseOnFilter extracts paths, tags, and excludes from raw on strings.
func ParseOnFilter(raw []string) OnFilter {
	var f OnFilter
	for _, s := range raw {
		switch {
		case strings.HasPrefix(s, "!#"):
			// exclude tag
			f.Excludes = append(f.Excludes, Exclude{Tag: s[2:]})
		case strings.HasPrefix(s, "!"):
			// exclude path
			f.Excludes = append(f.Excludes, Exclude{Path: s[1:]})
		case strings.HasPrefix(s, "#"):
			// tag filter
			f.Tags = append(f.Tags, s[1:])
		default:
			// path glob
			f.Paths = append(f.Paths, s)
		}
	}
	return f
}
