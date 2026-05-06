package config

import (
	"path/filepath"

	"github.com/caoer/meridian/internal/rules"
)

// Profile selects and configures a subset of rules.
type Profile struct {
	Include  []string            `yaml:"include"`
	Exclude  []string            `yaml:"exclude"`
	Override map[string]Override `yaml:"override"`
}

// Override changes rule properties. Currently only severity.
type Override struct {
	Severity string `yaml:"severity"`
}

// Resolve applies the profile to a set of rules, returning the filtered+overridden result.
// Copies rules — does not mutate originals.
func (p Profile) Resolve(allRules []rules.Rule) []rules.Rule {
	include := p.Include
	if len(include) == 0 {
		include = []string{"*"}
	}

	var result []rules.Rule
	for _, r := range allRules {
		if !matchAny(r.ID, include) {
			continue
		}
		if matchAny(r.ID, p.Exclude) {
			continue
		}

		// Copy to avoid mutating original
		resolved := r
		if ov, ok := p.Override[r.ID]; ok {
			if ov.Severity != "" {
				if sev, err := rules.ParseSeverity(ov.Severity); err == nil {
					resolved.Severity = sev
				}
			}
		}
		result = append(result, resolved)
	}
	return result
}

// matchAny checks if id matches any glob pattern.
func matchAny(id string, patterns []string) bool {
	for _, pat := range patterns {
		if matched, _ := filepath.Match(pat, id); matched {
			return true
		}
	}
	return false
}
