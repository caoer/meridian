package property

import "fmt"

// Finding is what a type block evaluator produces.
type Finding struct {
	Line         int
	TemplateData map[string]string
}

// EvalContext provides cross-file context to evaluators.
type EvalContext struct {
	ScannedPaths []string        // all scanned file paths (for resolve)
	FilePath     string          // current document path
	GitRoot      string          // git repo root (for freshness checks)
	GitTimeCache map[string]int64 // cache of git timestamps (path → unix time)
}

// TypeBlock evaluates a frontmatter value against type-specific predicates.
type TypeBlock interface {
	Evaluate(key string, value any, ctx *EvalContext) []Finding
}

// TypeBlockParser converts raw YAML config into a TypeBlock.
type TypeBlockParser func(raw any) (TypeBlock, error)

// Config is parsed from a property rule's Params map.
type Config struct {
	Keys     []string // from property: field (string -> single, []string -> multi)
	Required bool     // from required: field
	TypeName string   // "wikilink", "tag", "date", "text", or "" (existence-only)
	TypeRaw  any      // raw YAML value for the type block
}

// ResolvedConfig has a concrete TypeBlock ready for evaluation.
type ResolvedConfig struct {
	Keys     []string
	Required bool
	Type     TypeBlock // resolved from TypeRaw via parser
}

// Parse builds a Config from a rule's Params map.
func Parse(params map[string]any) (*Config, error) {
	c := &Config{}

	// Extract keys from "property" field
	prop, ok := params["property"]
	if !ok {
		return nil, fmt.Errorf("missing 'property' in params")
	}
	switch v := prop.(type) {
	case string:
		c.Keys = []string{v}
	case []any:
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("property[%d]: expected string, got %T", i, item)
			}
			c.Keys = append(c.Keys, s)
		}
		if len(c.Keys) == 0 {
			return nil, fmt.Errorf("property: empty array")
		}
	default:
		return nil, fmt.Errorf("property: expected string or array, got %T", prop)
	}

	// Extract required (default false)
	if rv, ok := params["required"]; ok {
		b, ok := rv.(bool)
		if !ok {
			return nil, fmt.Errorf("required: expected bool, got %T", rv)
		}
		c.Required = b
	}

	// Detect type block — at most one expected (loader already validates this)
	for _, name := range []string{"wikilink", "tag", "date", "text"} {
		if raw, ok := params[name]; ok {
			c.TypeName = name
			c.TypeRaw = raw
			break
		}
	}

	return c, nil
}

// ResolveType parses the raw type block using the given parser registry.
// Returns a resolved config ready for evaluation, or error if parsing fails.
func (c *Config) ResolveType(parsers map[string]TypeBlockParser) (*ResolvedConfig, error) {
	rc := &ResolvedConfig{
		Keys:     c.Keys,
		Required: c.Required,
	}

	if c.TypeName == "" {
		// Existence-only check — no type block to resolve
		return rc, nil
	}

	parser, ok := parsers[c.TypeName]
	if !ok {
		return nil, fmt.Errorf("no parser registered for type block %q", c.TypeName)
	}

	tb, err := parser(c.TypeRaw)
	if err != nil {
		return nil, fmt.Errorf("parsing type block %q: %w", c.TypeName, err)
	}
	rc.Type = tb

	return rc, nil
}
