package property

import (
	"testing"
)

func TestParseText_Bool(t *testing.T) {
	tb, err := ParseText(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tb == nil {
		t.Fatal("expected non-nil TypeBlock")
	}
}

func TestParseText_BoolFalse(t *testing.T) {
	_, err := ParseText(false)
	if err == nil {
		t.Fatal("expected error for false")
	}
}

func TestParseText_MatchRegex(t *testing.T) {
	raw := map[string]any{
		"match": "^[A-Z]",
	}
	tb, err := ParseText(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := tb.(*TextBlock)
	if text.matchRe == nil {
		t.Fatal("expected compiled regex")
	}
}

func TestParseText_InList(t *testing.T) {
	raw := map[string]any{
		"in": []any{"draft", "published", "archived"},
	}
	tb, err := ParseText(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := tb.(*TextBlock)
	if len(text.In) != 3 {
		t.Fatalf("expected 3 values, got %d", len(text.In))
	}
}

func TestParseText_BadRegex(t *testing.T) {
	raw := map[string]any{
		"match": "[invalid",
	}
	_, err := ParseText(raw)
	if err == nil {
		t.Fatal("expected error for bad regex")
	}
}

func TestParseText_InvalidType(t *testing.T) {
	_, err := ParseText(42)
	if err == nil {
		t.Fatal("expected error for int")
	}
}

func TestTextEvaluate_MatchesRegex(t *testing.T) {
	tb, _ := ParseText(map[string]any{"match": "^[A-Z]"})
	findings := tb.Evaluate("title", "Hello World", nil)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(findings))
	}
}

func TestTextEvaluate_NoMatchRegex(t *testing.T) {
	tb, _ := ParseText(map[string]any{"match": "^[A-Z]"})
	findings := tb.Evaluate("title", "hello world", nil)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Key"] != "title" {
		t.Errorf("expected Key=title, got %q", findings[0].TemplateData["Key"])
	}
}

func TestTextEvaluate_InEnum(t *testing.T) {
	tb, _ := ParseText(map[string]any{"in": []any{"draft", "published"}})
	f1 := tb.Evaluate("status", "draft", nil)
	if len(f1) != 0 {
		t.Fatalf("expected no findings for valid enum, got %d", len(f1))
	}
}

func TestTextEvaluate_NotInEnum(t *testing.T) {
	tb, _ := ParseText(map[string]any{"in": []any{"draft", "published"}})
	f := tb.Evaluate("status", "deleted", nil)
	if len(f) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(f))
	}
}

func TestTextEvaluate_BothMatchAndIn(t *testing.T) {
	tb, _ := ParseText(map[string]any{
		"match": "^[a-z]+$",
		"in":    []any{"draft", "published"},
	})

	// passes both
	f1 := tb.Evaluate("status", "draft", nil)
	if len(f1) != 0 {
		t.Fatalf("expected no findings, got %d", len(f1))
	}

	// fails match (uppercase)
	f2 := tb.Evaluate("status", "DRAFT", nil)
	if len(f2) != 2 {
		// fails regex + fails in (DRAFT != draft)
		t.Fatalf("expected 2 findings (regex + enum), got %d", len(f2))
	}

	// passes match, fails in
	f3 := tb.Evaluate("status", "deleted", nil)
	if len(f3) != 1 {
		t.Fatalf("expected 1 finding (enum only), got %d", len(f3))
	}
}

func TestTextEvaluate_NoConstraints(t *testing.T) {
	tb, _ := ParseText(true)
	findings := tb.Evaluate("title", "anything goes", nil)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(findings))
	}
}

func TestTextEvaluate_NonStringCoercion(t *testing.T) {
	tb, _ := ParseText(map[string]any{"match": "^42$"})
	findings := tb.Evaluate("count", 42, nil)
	if len(findings) != 0 {
		t.Fatalf("expected no findings (coerced 42 to string), got %d", len(findings))
	}
}
