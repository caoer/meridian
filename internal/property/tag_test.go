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
