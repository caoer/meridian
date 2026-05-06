package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v3"
)

// metaFields are the 4 required fields every rule must have.
var metaFields = map[string]bool{
	"check":    true,
	"message":  true,
	"severity": true,
	"on":       true,
}

// LoadDir loads all .yaml/.yml rule files from a directory.
// Returns parsed rules, warnings, and any fatal error.
func LoadDir(dir string) ([]Rule, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("reading rule dir %s: %w", dir, err)
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

		r, warns, err := loadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, nil, fmt.Errorf("rule %s: %w", e.Name(), err)
		}
		warnings = append(warnings, warns...)
		rules = append(rules, r)
	}

	return rules, warnings, nil
}

// maxYAMLAliases is the maximum number of anchor aliases allowed in a rule file.
const maxYAMLAliases = 100

// maxYAMLDepth is the maximum nesting depth allowed in a rule file.
const maxYAMLDepth = 10

// validateYAMLComplexity checks that a YAML document does not exceed alias or depth limits.
func validateYAMLComplexity(data []byte) error {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return fmt.Errorf("YAML parse: %w", err)
	}
	aliases := 0
	maxDepth := 0
	var walk func(n *yaml.Node, depth int)
	walk = func(n *yaml.Node, depth int) {
		if n == nil {
			return
		}
		if depth > maxDepth {
			maxDepth = depth
		}
		if n.Kind == yaml.AliasNode {
			aliases++
		}
		for _, c := range n.Content {
			walk(c, depth+1)
		}
	}
	walk(&node, 0)
	if aliases > maxYAMLAliases {
		return fmt.Errorf("YAML has %d aliases (max %d)", aliases, maxYAMLAliases)
	}
	if maxDepth > maxYAMLDepth {
		return fmt.Errorf("YAML nesting depth %d exceeds max %d", maxDepth, maxYAMLDepth)
	}
	return nil
}

// loadFile loads a single rule YAML file.
func loadFile(path string) (Rule, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Rule{}, nil, err
	}

	// Validate YAML complexity before full unmarshal.
	if err := validateYAMLComplexity(data); err != nil {
		return Rule{}, nil, err
	}

	// Decode into raw map for validation
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Rule{}, nil, fmt.Errorf("YAML parse: %w", err)
	}

	// Validate required meta fields
	for _, field := range []string{"check", "message", "severity", "on"} {
		if _, ok := raw[field]; !ok {
			return Rule{}, nil, fmt.Errorf("missing required field %q", field)
		}
	}

	// Extract check
	check, ok := raw["check"].(string)
	if !ok {
		return Rule{}, nil, fmt.Errorf("check must be a string")
	}

	// Extract message and validate template
	message, ok := raw["message"].(string)
	if !ok {
		return Rule{}, nil, fmt.Errorf("message must be a string")
	}
	if _, err := template.New("").Parse(message); err != nil {
		return Rule{}, nil, fmt.Errorf("invalid template in message: %w", err)
	}

	// Extract severity
	sevStr, ok := raw["severity"].(string)
	if !ok {
		return Rule{}, nil, fmt.Errorf("severity must be a string")
	}
	sev, err := ParseSeverity(sevStr)
	if err != nil {
		return Rule{}, nil, err
	}

	// Extract on — string or []any
	onFilter, err := parseOnField(raw["on"])
	if err != nil {
		return Rule{}, nil, fmt.Errorf("invalid on: %w", err)
	}

	// Validate globs
	if err := validateGlobs(onFilter); err != nil {
		return Rule{}, nil, err
	}

	// Collect check-specific params (everything not in meta)
	params := make(map[string]any)
	for k, v := range raw {
		if !metaFields[k] {
			params[k] = v
		}
	}

	// ID from filename stem
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	return Rule{
		ID:       id,
		Check:    check,
		Message:  message,
		Severity: sev,
		On:       onFilter,
		Params:   params,
	}, nil, nil
}

// parseOnField converts the raw `on` value to an OnFilter.
func parseOnField(v any) (OnFilter, error) {
	switch val := v.(type) {
	case string:
		return ParseOnFilter([]string{val}), nil
	case []any:
		var strs []string
		for _, item := range val {
			s, ok := item.(string)
			if !ok {
				return OnFilter{}, fmt.Errorf("on array element must be string, got %T", item)
			}
			strs = append(strs, s)
		}
		return ParseOnFilter(strs), nil
	default:
		return OnFilter{}, fmt.Errorf("on must be string or array, got %T", v)
	}
}

// validateGlobs checks that all path globs and exclude paths are valid doublestar patterns.
func validateGlobs(f OnFilter) error {
	for _, p := range f.Paths {
		if !doublestar.ValidatePattern(p) {
			return fmt.Errorf("invalid glob pattern: %q", p)
		}
	}
	for _, ex := range f.Excludes {
		if ex.Path != "" && !doublestar.ValidatePattern(ex.Path) {
			return fmt.Errorf("invalid exclude glob: %q", ex.Path)
		}
	}
	return nil
}

// DetectDuplicates returns an error if any two rules share an ID.
func DetectDuplicates(rules []Rule) error {
	seen := make(map[string]bool, len(rules))
	for _, r := range rules {
		if seen[r.ID] {
			return fmt.Errorf("duplicate rule ID: %q", r.ID)
		}
		seen[r.ID] = true
	}
	return nil
}
