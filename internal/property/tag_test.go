package property

import (
	"regexp"
	"testing"
)

func TestParseTag_Bool(t *testing.T) {
	tb, err := ParseTag(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tb == nil {
		t.Fatal("expected non-nil TypeBlock")
	}
}

func TestParseTag_BoolFalse(t *testing.T) {
	_, err := ParseTag(false)
	if err == nil {
		t.Fatal("expected error for false")
	}
}

func TestParseTag_PrefixIn(t *testing.T) {
	raw := map[string]any{
		"prefix": map[string]any{
			"in": []any{"type", "domain"},
		},
	}
	tb, err := ParseTag(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tag := tb.(*TagBlock)
	if len(tag.PrefixIn) != 2 {
		t.Fatalf("expected 2 prefixes, got %d", len(tag.PrefixIn))
	}
}

func TestParseTag_PrefixMatch(t *testing.T) {
	raw := map[string]any{
		"prefix": map[string]any{
			"match": "^(type|domain)$",
		},
	}
	tb, err := ParseTag(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tag := tb.(*TagBlock)
	if tag.prefixRe == nil {
		t.Fatal("expected compiled regex")
	}
}

func TestParseTag_ValueIn(t *testing.T) {
	raw := map[string]any{
		"value": map[string]any{
			"in": []any{"source", "security"},
		},
	}
	tb, err := ParseTag(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tag := tb.(*TagBlock)
	if len(tag.ValueIn) != 2 {
		t.Fatalf("expected 2 values, got %d", len(tag.ValueIn))
	}
}

func TestParseTag_BadRegex(t *testing.T) {
	raw := map[string]any{
		"prefix": map[string]any{
			"match": "[invalid",
		},
	}
	_, err := ParseTag(raw)
	if err == nil {
		t.Fatal("expected error for bad regex")
	}
}

func TestParseTag_BadValueRegex(t *testing.T) {
	raw := map[string]any{
		"value": map[string]any{
			"match": "[invalid",
		},
	}
	_, err := ParseTag(raw)
	if err == nil {
		t.Fatal("expected error for bad regex")
	}
}

func TestParseTag_InvalidType(t *testing.T) {
	_, err := ParseTag(42)
	if err == nil {
		t.Fatal("expected error for int")
	}
}

func TestTagEvaluate_ValidKnownPrefix(t *testing.T) {
	tb := &TagBlock{
		PrefixIn: []string{"type", "domain"},
	}
	findings := tb.Evaluate("tags", "type/source", nil)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(findings))
	}
}

func TestTagEvaluate_UnknownPrefix(t *testing.T) {
	tb := &TagBlock{
		PrefixIn: []string{"type", "domain"},
	}
	findings := tb.Evaluate("tags", "unknown/value", nil)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Prefix"] != "unknown" {
		t.Errorf("expected Prefix=unknown, got %q", findings[0].TemplateData["Prefix"])
	}
}

func TestTagEvaluate_NoSlash(t *testing.T) {
	tb := &TagBlock{}
	findings := tb.Evaluate("tags", "noslash", nil)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Value"] != "noslash" {
		t.Errorf("expected Value=noslash, got %q", findings[0].TemplateData["Value"])
	}
}

func TestTagEvaluate_ListMixed(t *testing.T) {
	tb := &TagBlock{
		PrefixIn: []string{"type"},
	}
	value := []any{"type/source", "bad/value", "noslash"}
	findings := tb.Evaluate("tags", value, nil)
	// bad/value → prefix not in list (1 finding)
	// noslash → no slash (1 finding)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
}

func TestTagEvaluate_PrefixRegexMatch(t *testing.T) {
	re := "^type$"
	compiled, _ := compileRe(re)
	tb := &TagBlock{
		PrefixMatch: re,
		prefixRe:    compiled,
	}
	findings := tb.Evaluate("tags", "type/source", nil)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(findings))
	}
}

func TestTagEvaluate_PrefixRegexNoMatch(t *testing.T) {
	re := "^type$"
	compiled, _ := compileRe(re)
	tb := &TagBlock{
		PrefixMatch: re,
		prefixRe:    compiled,
	}
	findings := tb.Evaluate("tags", "domain/security", nil)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestTagEvaluate_ValueIn(t *testing.T) {
	tb := &TagBlock{
		ValueIn: []string{"source", "security"},
	}
	// valid
	f1 := tb.Evaluate("tags", "type/source", nil)
	if len(f1) != 0 {
		t.Fatalf("expected no findings for valid value, got %d", len(f1))
	}
	// invalid
	f2 := tb.Evaluate("tags", "type/unknown", nil)
	if len(f2) != 1 {
		t.Fatalf("expected 1 finding for invalid value, got %d", len(f2))
	}
}

func TestTagEvaluate_ValueMatch(t *testing.T) {
	re := "^[a-z]+$"
	compiled, _ := compileRe(re)
	tb := &TagBlock{
		ValueMatch: re,
		valueRe:    compiled,
	}
	// match
	f1 := tb.Evaluate("tags", "type/source", nil)
	if len(f1) != 0 {
		t.Fatalf("expected no findings, got %d", len(f1))
	}
	// no match
	f2 := tb.Evaluate("tags", "type/123", nil)
	if len(f2) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(f2))
	}
}

func TestTagEvaluate_StringValue(t *testing.T) {
	tb := &TagBlock{}
	findings := tb.Evaluate("tags", "type/source", nil)
	if len(findings) != 0 {
		t.Fatalf("expected no findings for valid tag string, got %d", len(findings))
	}
}

func TestParseTag_NestedPrefixAndValue(t *testing.T) {
	// Matches real YAML: tag: { prefix: { in: [...] }, value: { match: "..." } }
	raw := map[string]any{
		"prefix": map[string]any{
			"in": []any{"domain", "type"},
		},
		"value": map[string]any{
			"match": "^[a-z-]+$",
		},
	}
	tb, err := ParseTag(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tag := tb.(*TagBlock)
	if len(tag.PrefixIn) != 2 {
		t.Fatalf("expected 2 prefixes, got %d", len(tag.PrefixIn))
	}
	if tag.valueRe == nil {
		t.Fatal("expected compiled value regex")
	}

	// Valid: known prefix + lowercase value
	f1 := tag.Evaluate("tags", "domain/security", nil)
	if len(f1) != 0 {
		t.Fatalf("expected no findings, got %d", len(f1))
	}

	// Invalid prefix
	f2 := tag.Evaluate("tags", "unknown/security", nil)
	if len(f2) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(f2))
	}

	// Invalid value (uppercase)
	f3 := tag.Evaluate("tags", "domain/Security", nil)
	if len(f3) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(f3))
	}
}

func TestTagEvaluate_PrefixInAndMatchNoDuplicate(t *testing.T) {
	// P1-5: when both PrefixIn and PrefixMatch are configured, a failing tag
	// should produce exactly 1 finding per predicate group, not 2.
	re := "^type$"
	compiled, _ := compileRe(re)
	tb := &TagBlock{
		PrefixIn:    []string{"type"},
		PrefixMatch: re,
		prefixRe:    compiled,
	}
	findings := tb.Evaluate("tags", "bad/value", nil)
	prefixFindings := 0
	for _, f := range findings {
		if f.TemplateData["Prefix"] == "bad" {
			prefixFindings++
		}
	}
	if prefixFindings != 1 {
		t.Fatalf("expected exactly 1 prefix finding, got %d", prefixFindings)
	}
}

func TestTagEvaluate_ValueInAndMatchNoDuplicate(t *testing.T) {
	re := "^source$"
	compiled, _ := compileRe(re)
	tb := &TagBlock{
		ValueIn:    []string{"source"},
		ValueMatch: re,
		valueRe:    compiled,
	}
	findings := tb.Evaluate("tags", "type/bad", nil)
	valueFindings := 0
	for _, f := range findings {
		if f.TemplateData["TagValue"] == "bad" {
			valueFindings++
		}
	}
	if valueFindings != 1 {
		t.Fatalf("expected exactly 1 value finding, got %d", valueFindings)
	}
}

func TestTagEvaluate_ReasonFields(t *testing.T) {
	// P1-8: findings must include Reason in TemplateData.
	tests := []struct {
		name   string
		tb     *TagBlock
		tag    string
		reason string
	}{
		{
			name:   "no slash",
			tb:     &TagBlock{},
			tag:    "noslash",
			reason: "tag missing prefix/value separator",
		},
		{
			name:   "unknown prefix",
			tb:     &TagBlock{PrefixIn: []string{"type"}},
			tag:    "bad/value",
			reason: `unknown prefix "bad"`,
		},
		{
			name: "prefix match fail",
			tb: func() *TagBlock {
				re, _ := compileRe("^type$")
				return &TagBlock{PrefixMatch: "^type$", prefixRe: re}
			}(),
			tag:    "bad/value",
			reason: `prefix does not match pattern "^type$"`,
		},
		{
			name:   "unknown value",
			tb:     &TagBlock{ValueIn: []string{"source"}},
			tag:    "type/bad",
			reason: `unknown value "bad"`,
		},
		{
			name: "value match fail",
			tb: func() *TagBlock {
				re, _ := compileRe("^[a-z]+$")
				return &TagBlock{ValueMatch: "^[a-z]+$", valueRe: re}
			}(),
			tag:    "type/123",
			reason: `value does not match pattern "^[a-z]+$"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := tt.tb.Evaluate("tags", tt.tag, nil)
			if len(findings) == 0 {
				t.Fatal("expected at least 1 finding")
			}
			reason := findings[0].TemplateData["Reason"]
			if reason != tt.reason {
				t.Errorf("Reason = %q, want %q", reason, tt.reason)
			}
		})
	}
}

func TestParseTag_PrefixNotMap(t *testing.T) {
	raw := map[string]any{
		"prefix": "not-a-map",
	}
	_, err := ParseTag(raw)
	if err == nil {
		t.Fatal("expected error when prefix is not a map")
	}
}

func compileRe(s string) (*regexp.Regexp, error) {
	return regexp.Compile(s)
}
