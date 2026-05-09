package property

import (
	"fmt"
	"regexp"
	"strings"
)

// TagBlock implements TypeBlock for tag-typed properties.
// Tags have format "prefix/value" (e.g., "type/source", "domain/security").
type TagBlock struct {
	PrefixIn    []string // allowed prefixes (nil = any)
	PrefixMatch string   // regex for prefix (empty = any)
	ValueIn     []string // allowed values (nil = any)
	ValueMatch  string   // regex for value (empty = any)

	prefixRe *regexp.Regexp
	valueRe  *regexp.Regexp
}

// ParseTag builds a TagBlock from raw YAML.
// Accepts true (bare validation) or map with prefix.in, prefix.match, value.in, value.match.
func ParseTag(raw any) (TypeBlock, error) {
	switch v := raw.(type) {
	case bool:
		if !v {
			return nil, fmt.Errorf("tag: false not valid")
		}
		return &TagBlock{}, nil

	case map[string]any:
		tb := &TagBlock{}

		// prefix: {in: [...], match: "..."}
		if pRaw, ok := v["prefix"]; ok {
			pm, ok := pRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("prefix: expected map, got %T", pRaw)
			}
			if pi, ok := pm["in"]; ok {
				list, err := toStringSlice(pi, "prefix.in")
				if err != nil {
					return nil, err
				}
				tb.PrefixIn = list
			}
			if pmatch, ok := pm["match"]; ok {
				s, ok := pmatch.(string)
				if !ok {
					return nil, fmt.Errorf("prefix.match: expected string, got %T", pmatch)
				}
				tb.PrefixMatch = s
				re, err := regexp.Compile(s)
				if err != nil {
					return nil, fmt.Errorf("prefix.match: bad regex: %w", err)
				}
				tb.prefixRe = re
			}
		}

		// value: {in: [...], match: "..."}
		if vRaw, ok := v["value"]; ok {
			vm, ok := vRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("value: expected map, got %T", vRaw)
			}
			if vi, ok := vm["in"]; ok {
				list, err := toStringSlice(vi, "value.in")
				if err != nil {
					return nil, err
				}
				tb.ValueIn = list
			}
			if vmatch, ok := vm["match"]; ok {
				s, ok := vmatch.(string)
				if !ok {
					return nil, fmt.Errorf("value.match: expected string, got %T", vmatch)
				}
				tb.ValueMatch = s
				re, err := regexp.Compile(s)
				if err != nil {
					return nil, fmt.Errorf("value.match: bad regex: %w", err)
				}
				tb.valueRe = re
			}
		}

		return tb, nil

	default:
		return nil, fmt.Errorf("tag: expected bool or map, got %T", raw)
	}
}

// Evaluate checks tag values against configured constraints.
func (tb *TagBlock) Evaluate(key string, value any, ctx *EvalContext) []Finding {
	tags := toTagList(value)
	var findings []Finding

	for _, tag := range tags {
		findings = append(findings, tb.evaluateOne(key, tag)...)
	}
	return findings
}

func (tb *TagBlock) evaluateOne(key, tag string) []Finding {
	idx := strings.IndexByte(tag, '/')
	if idx < 0 {
		m := map[string]string{
			"Key":    key,
			"Value":  tag,
			"Reason": "tag missing prefix/value separator",
		}
		return []Finding{{TemplateData: m}}
	}

	prefix := tag[:idx]
	tagValue := tag[idx+1:]
	base := map[string]string{
		"Key":      key,
		"Value":    tag,
		"Prefix":   prefix,
		"TagValue": tagValue,
	}

	var findings []Finding

	prefixFailed := false
	if tb.PrefixIn != nil && !contains(tb.PrefixIn, prefix) {
		m := copyMap(base)
		m["Reason"] = fmt.Sprintf("unknown prefix %q", prefix)
		findings = append(findings, Finding{TemplateData: m})
		prefixFailed = true
	}

	if !prefixFailed && tb.prefixRe != nil && !tb.prefixRe.MatchString(prefix) {
		m := copyMap(base)
		m["Reason"] = fmt.Sprintf("prefix does not match pattern %q", tb.PrefixMatch)
		findings = append(findings, Finding{TemplateData: m})
	}

	valueFailed := false
	if tb.ValueIn != nil && !contains(tb.ValueIn, tagValue) {
		m := copyMap(base)
		m["Reason"] = fmt.Sprintf("unknown value %q", tagValue)
		findings = append(findings, Finding{TemplateData: m})
		valueFailed = true
	}

	if !valueFailed && tb.valueRe != nil && !tb.valueRe.MatchString(tagValue) {
		m := copyMap(base)
		m["Reason"] = fmt.Sprintf("value does not match pattern %q", tb.ValueMatch)
		findings = append(findings, Finding{TemplateData: m})
	}

	return findings
}

// toTagList normalizes value into a list of tag strings.
func toTagList(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	default:
		return []string{fmt.Sprintf("%v", value)}
	}
}

// toStringSlice converts []any to []string for config parsing.
func toStringSlice(v any, field string) ([]string, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected array, got %T", field, v)
	}
	out := make([]string, 0, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d]: expected string, got %T", field, i, item)
		}
		out = append(out, s)
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
