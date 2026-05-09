package property

import (
	"fmt"
	"regexp"
)

// TextBlock implements TypeBlock for text-typed properties.
type TextBlock struct {
	Match string   // regex pattern (empty = any)
	In    []string // allowed values (nil = any)

	matchRe *regexp.Regexp
}

// ParseText builds a TextBlock from raw YAML.
// Accepts true (no constraints) or map with match and/or in keys.
func ParseText(raw any) (TypeBlock, error) {
	switch v := raw.(type) {
	case bool:
		if !v {
			return nil, fmt.Errorf("text: false not valid")
		}
		return &TextBlock{}, nil

	case map[string]any:
		tb := &TextBlock{}

		if m, ok := v["match"]; ok {
			s, ok := m.(string)
			if !ok {
				return nil, fmt.Errorf("text.match: expected string, got %T", m)
			}
			tb.Match = s
			re, err := regexp.Compile(s)
			if err != nil {
				return nil, fmt.Errorf("text.match: bad regex: %w", err)
			}
			tb.matchRe = re
		}

		if in, ok := v["in"]; ok {
			list, err := toStringSlice(in, "text.in")
			if err != nil {
				return nil, err
			}
			tb.In = list
		}

		return tb, nil

	default:
		return nil, fmt.Errorf("text: expected bool or map, got %T", raw)
	}
}

// Evaluate checks value against text constraints.
func (tb *TextBlock) Evaluate(key string, value any, ctx *EvalContext) []Finding {
	s := fmt.Sprintf("%v", value)

	var findings []Finding

	if tb.matchRe != nil && !tb.matchRe.MatchString(s) {
		findings = append(findings, Finding{
			TemplateData: map[string]string{
				"Key":    key,
				"Value":  s,
				"Reason": fmt.Sprintf("does not match pattern %q", tb.Match),
			},
		})
	}

	if tb.In != nil && !contains(tb.In, s) {
		findings = append(findings, Finding{
			TemplateData: map[string]string{
				"Key":    key,
				"Value":  s,
				"Reason": fmt.Sprintf("not in allowed values %v", tb.In),
			},
		})
	}

	return findings
}
