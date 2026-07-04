package rules

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"text/template"

	"go.yaml.in/yaml/v3"
)

// LoadFS loads all .yaml/.yml rule files from an fs.FS (e.g. embedded).
// Returns parsed rules, warnings, and any fatal error.
func LoadFS(fsys fs.FS) ([]Rule, []string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("reading rule dir: %w", err)
	}

	var rules []Rule
	var warnings []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		r, warns, err := loadFileFS(fsys, e.Name())
		if err != nil {
			return nil, nil, fmt.Errorf("rule %s: %w", e.Name(), err)
		}
		warnings = append(warnings, warns...)
		rules = append(rules, r)
	}

	return rules, warnings, nil
}

// loadFileFS loads a single rule YAML file from an fs.FS.
func loadFileFS(fsys fs.FS, name string) (Rule, []string, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Rule{}, nil, err
	}

	if err := validateYAMLComplexity(data); err != nil {
		return Rule{}, nil, err
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Rule{}, nil, fmt.Errorf("YAML parse: %w", err)
	}

	_, hasCheck := raw["check"]
	_, hasProperty := raw["property"]

	switch {
	case hasCheck && hasProperty:
		return Rule{}, nil, fmt.Errorf("rule has both 'check' and 'property'")
	case hasProperty:
		return loadPropertyRuleFS(raw, name)
	case hasCheck:
		return loadCheckRuleFS(raw, name)
	default:
		return Rule{}, nil, fmt.Errorf("rule must have 'check' or 'property'")
	}
}

// loadCheckRuleFS is the fs.FS variant — identical logic, uses name for ID.
func loadCheckRuleFS(raw map[string]any, name string) (Rule, []string, error) {
	for _, field := range []string{"check", "message", "severity", "on"} {
		if _, ok := raw[field]; !ok {
			return Rule{}, nil, fmt.Errorf("missing required field %q", field)
		}
	}

	check, ok := raw["check"].(string)
	if !ok {
		return Rule{}, nil, fmt.Errorf("check must be a string")
	}

	message, ok := raw["message"].(string)
	if !ok {
		return Rule{}, nil, fmt.Errorf("message must be a string")
	}
	if _, err := template.New("").Parse(message); err != nil {
		return Rule{}, nil, fmt.Errorf("invalid template in message: %w", err)
	}

	sevStr, ok := raw["severity"].(string)
	if !ok {
		return Rule{}, nil, fmt.Errorf("severity must be a string")
	}
	sev, err := ParseSeverity(sevStr)
	if err != nil {
		return Rule{}, nil, err
	}

	onFilter, err := parseOnField(raw["on"])
	if err != nil {
		return Rule{}, nil, fmt.Errorf("invalid on: %w", err)
	}
	if err := validateGlobs(onFilter); err != nil {
		return Rule{}, nil, err
	}

	params := make(map[string]any)
	for k, v := range raw {
		if !metaFields[k] {
			params[k] = v
		}
	}

	id := strings.TrimSuffix(name, filepath.Ext(name))

	return Rule{
		ID:       id,
		Check:    check,
		Message:  message,
		Severity: sev,
		On:       onFilter,
		Params:   params,
	}, nil, nil
}

// loadPropertyRuleFS is the fs.FS variant for property rules.
func loadPropertyRuleFS(raw map[string]any, name string) (Rule, []string, error) {
	var warnings []string

	if _, ok := raw["on"]; !ok {
		return Rule{}, nil, fmt.Errorf("missing required field %q", "on")
	}
	onFilter, err := parseOnField(raw["on"])
	if err != nil {
		return Rule{}, nil, fmt.Errorf("invalid on: %w", err)
	}
	if err := validateGlobs(onFilter); err != nil {
		return Rule{}, nil, err
	}

	sev := SeverityWarn
	if sv, ok := raw["severity"]; ok {
		sevStr, ok := sv.(string)
		if !ok {
			return Rule{}, nil, fmt.Errorf("severity must be a string")
		}
		sev, err = ParseSeverity(sevStr)
		if err != nil {
			return Rule{}, nil, err
		}
	}

	message := "{{.Message}}"
	if mv, ok := raw["message"]; ok {
		ms, ok := mv.(string)
		if !ok {
			return Rule{}, nil, fmt.Errorf("message must be a string")
		}
		if _, err := template.New("").Parse(ms); err != nil {
			return Rule{}, nil, fmt.Errorf("invalid template in message: %w", err)
		}
		message = ms
	}

	params := make(map[string]any)
	params["property"] = raw["property"]
	if rv, ok := raw["required"]; ok {
		params["required"] = rv
	} else {
		params["required"] = false
	}

	var typeBlockCount int
	for k, v := range raw {
		if typeBlockNames[k] {
			typeBlockCount++
			params[k] = v
		}
	}
	if typeBlockCount > 1 {
		return Rule{}, nil, fmt.Errorf("property rule has multiple type blocks")
	}

	for k, v := range raw {
		if propertyMetaFields[k] || typeBlockNames[k] {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("unknown field %q in property rule", k))
		params[k] = v
	}

	id := strings.TrimSuffix(name, filepath.Ext(name))

	return Rule{
		ID:       id,
		Check:    "property",
		Message:  message,
		Severity: sev,
		On:       onFilter,
		Params:   params,
	}, warnings, nil
}
