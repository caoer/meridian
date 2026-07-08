package rules

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v3"
)

// metaFields are the 4 required fields every check rule must have.
var metaFields = map[string]bool{
	"check":    true,
	"message":  true,
	"severity": true,
	"on":       true,
}

// propertyMetaFields are top-level fields consumed by the loader for property rules.
var propertyMetaFields = map[string]bool{
	"property": true,
	"on":       true,
	"required": true,
	"severity": true,
	"message":  true,
}

// typeBlockNames are the recognized type block keys for property rules.
var typeBlockNames = map[string]bool{
	"wikilink": true,
	"tag":      true,
	"date":     true,
	"text":     true,
}

// LoadDir loads all rule files from a directory: .yaml/.yml plain rules and
// .md literate rule pages (frontmatter `md-rule:` → ^id yaml fence).
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
		path := filepath.Join(dir, e.Name())
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".yaml", ".yml":
			r, warns, err := loadFile(path)
			if err != nil {
				return nil, nil, fmt.Errorf("rule %s: %w", e.Name(), err)
			}
			warnings = append(warnings, warns...)
			rules = append(rules, r)
		case ".md":
			r, ok, warns, err := loadMarkdownFile(path)
			if err != nil {
				return nil, nil, fmt.Errorf("rule %s: %w", e.Name(), err)
			}
			warnings = append(warnings, warns...)
			if ok {
				rules = append(rules, r)
			}
		}
	}

	return rules, warnings, nil
}

// LoadFS loads all .yaml/.yml rules under dir in fsys — the embedded-pack
// twin of LoadDir. Literate .md rules are a disk convention and are not
// supported in embedded packs.
func LoadFS(fsys fs.FS, dir string) ([]Rule, []string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, nil, fmt.Errorf("reading rule dir %s: %w", dir, err)
	}

	var rules []Rule
	var warnings []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".yaml", ".yml":
			data, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
			if err != nil {
				return nil, nil, fmt.Errorf("rule %s: %w", e.Name(), err)
			}
			r, warns, err := loadYAMLBytes(data, e.Name())
			if err != nil {
				return nil, nil, fmt.Errorf("rule %s: %w", e.Name(), err)
			}
			warnings = append(warnings, warns...)
			rules = append(rules, r)
		}
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
	return loadYAMLBytes(data, path)
}

// loadYAMLBytes parses rule YAML — from a .yaml file or a literate page's
// yaml fence — and routes it to the check/property loader. The rule ID comes
// from path's filename stem.
func loadYAMLBytes(data []byte, path string) (Rule, []string, error) {
	// Validate YAML complexity before full unmarshal.
	if err := validateYAMLComplexity(data); err != nil {
		return Rule{}, nil, err
	}

	// Decode into raw map for validation
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
		return loadPropertyRule(raw, path)
	case hasCheck:
		return loadCheckRule(raw, path)
	default:
		return Rule{}, nil, fmt.Errorf("rule must have 'check' or 'property'")
	}
}

// loadCheckRule handles the existing check-rule path (unchanged behavior).
func loadCheckRule(raw map[string]any, path string) (Rule, []string, error) {
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

// loadPropertyRule builds a Rule from a property-style YAML definition.
func loadPropertyRule(raw map[string]any, path string) (Rule, []string, error) {
	var warnings []string

	// on is still required
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

	// severity: default to warn
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

	// message: default to "{{.Message}}" so the check function can supply contextual text.
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

	// Build params — everything not in propertyMetaFields
	params := make(map[string]any)
	params["property"] = raw["property"]

	// required: default false
	if rv, ok := raw["required"]; ok {
		params["required"] = rv
	} else {
		params["required"] = false
	}

	// Detect type blocks — at most one allowed
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

	// Remaining keys that are neither propertyMeta nor type blocks → params + warning
	for k, v := range raw {
		if propertyMetaFields[k] || typeBlockNames[k] {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("unknown field %q in property rule", k))
		params[k] = v
	}

	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	return Rule{
		ID:       id,
		Check:    "property",
		Message:  message,
		Severity: sev,
		On:       onFilter,
		Params:   params,
	}, warnings, nil
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
