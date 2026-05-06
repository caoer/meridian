package testkit

import "github.com/caoer/meridian/internal/rules"

// RuleOption configures a Rule.
type RuleOption func(*rules.Rule)

// Rule builds a rules.Rule with defaults + options.
func Rule(id string, opts ...RuleOption) rules.Rule {
	r := rules.Rule{
		ID:       id,
		Severity: rules.SeverityWarn,
		Params:   make(map[string]any),
	}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

// Check sets the check type.
func Check(name string) RuleOption {
	return func(r *rules.Rule) { r.Check = name }
}

// Severity sets severity (parses string to Severity type).
func Severity(s string) RuleOption {
	return func(r *rules.Rule) {
		sev, err := rules.ParseSeverity(s)
		if err != nil {
			panic("testkit: invalid severity: " + s)
		}
		r.Severity = sev
	}
}

// On sets the file filter (single glob string).
func On(pattern string) RuleOption {
	return func(r *rules.Rule) {
		r.On = rules.ParseOnFilter([]string{pattern})
	}
}

// OnTag appends a tag filter to the On filter.
func OnTag(tag string) RuleOption {
	return func(r *rules.Rule) {
		raw := onFilterToRaw(r.On)
		raw = append(raw, "#"+tag)
		r.On = rules.ParseOnFilter(raw)
	}
}

// OnExclude appends an exclude to the On filter.
func OnExclude(exclude string) RuleOption {
	return func(r *rules.Rule) {
		raw := onFilterToRaw(r.On)
		raw = append(raw, "!"+exclude)
		r.On = rules.ParseOnFilter(raw)
	}
}

// Frontmatter sets the frontmatter param (list of field names).
func Frontmatter(fields ...string) RuleOption {
	return func(r *rules.Rule) {
		ifaces := make([]any, len(fields))
		for i, f := range fields {
			ifaces[i] = f
		}
		r.Params["frontmatter"] = ifaces
	}
}

// Param sets a generic check-specific parameter.
func Param(key string, value any) RuleOption {
	return func(r *rules.Rule) {
		r.Params[key] = value
	}
}

// MessageTemplate sets the message template.
func MessageTemplate(tmpl string) RuleOption {
	return func(r *rules.Rule) { r.Message = tmpl }
}

// onFilterToRaw reconstructs raw strings from OnFilter.
func onFilterToRaw(f rules.OnFilter) []string {
	var raw []string
	raw = append(raw, f.Paths...)
	for _, t := range f.Tags {
		raw = append(raw, "#"+t)
	}
	for _, e := range f.Excludes {
		if e.Path != "" {
			raw = append(raw, "!"+e.Path)
		}
		if e.Tag != "" {
			raw = append(raw, "!#"+e.Tag)
		}
	}
	return raw
}
