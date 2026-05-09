package property

import (
	"testing"
	"time"
)

func TestParseDate_Bool(t *testing.T) {
	tb, err := ParseDate(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	db := tb.(*DateBlock)
	if db.Format != "2006-01-02" {
		t.Fatalf("expected default format, got %q", db.Format)
	}
}

func TestParseDate_BoolFalse(t *testing.T) {
	_, err := ParseDate(false)
	if err == nil {
		t.Fatal("expected error for false")
	}
}

func TestParseDate_CustomFormat(t *testing.T) {
	raw := map[string]any{
		"format": "01/02/2006",
	}
	tb, err := ParseDate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	db := tb.(*DateBlock)
	if db.Format != "01/02/2006" {
		t.Fatalf("expected custom format, got %q", db.Format)
	}
}

func TestParseDate_MapNoFormat(t *testing.T) {
	raw := map[string]any{}
	tb, err := ParseDate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	db := tb.(*DateBlock)
	if db.Format != "2006-01-02" {
		t.Fatalf("expected default format, got %q", db.Format)
	}
}

func TestParseDate_InvalidType(t *testing.T) {
	_, err := ParseDate(42)
	if err == nil {
		t.Fatal("expected error for int")
	}
}

func TestParseDate_BadFormatType(t *testing.T) {
	raw := map[string]any{
		"format": 123,
	}
	_, err := ParseDate(raw)
	if err == nil {
		t.Fatal("expected error for non-string format")
	}
}

func TestDateEvaluate_ValidDate(t *testing.T) {
	db := &DateBlock{Format: "2006-01-02"}
	findings := db.Evaluate("created", "2026-01-15", nil)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(findings))
	}
}

func TestDateEvaluate_InvalidDate(t *testing.T) {
	db := &DateBlock{Format: "2006-01-02"}
	findings := db.Evaluate("created", "not-a-date", nil)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Key"] != "created" {
		t.Errorf("expected Key=created, got %q", findings[0].TemplateData["Key"])
	}
	if findings[0].TemplateData["Value"] != "not-a-date" {
		t.Errorf("expected Value=not-a-date, got %q", findings[0].TemplateData["Value"])
	}
}

func TestDateEvaluate_WrongFormat(t *testing.T) {
	db := &DateBlock{Format: "2006-01-02"}
	findings := db.Evaluate("created", "01/15/2026", nil)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestDateEvaluate_CustomFormat(t *testing.T) {
	db := &DateBlock{Format: "01/02/2006"}
	findings := db.Evaluate("created", "01/15/2026", nil)
	if len(findings) != 0 {
		t.Fatalf("expected no findings for valid custom format, got %d", len(findings))
	}
}

func TestDateEvaluate_NonString(t *testing.T) {
	db := &DateBlock{Format: "2006-01-02"}
	findings := db.Evaluate("created", 12345, nil)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for non-string, got %d", len(findings))
	}
	if findings[0].TemplateData["Reason"] == "" {
		t.Error("expected Reason in TemplateData")
	}
}

func TestDateEvaluate_TimeTime(t *testing.T) {
	// YAML parses unquoted dates like `created: 2026-04-11` as time.Time.
	// DateBlock must accept these without findings.
	db := &DateBlock{Format: "2006-01-02"}
	val := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	findings := db.Evaluate("created", val, nil)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for time.Time, got %d: %v", len(findings), findings[0].TemplateData)
	}
}

func TestDateEvaluate_TimeTimeWithTimezone(t *testing.T) {
	// YAML may parse with timezone info — still valid.
	db := &DateBlock{Format: "2006-01-02"}
	loc, _ := time.LoadLocation("America/New_York")
	val := time.Date(2026, 4, 11, 15, 30, 0, 0, loc)
	findings := db.Evaluate("created", val, nil)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for time.Time with tz, got %d: %v", len(findings), findings[0].TemplateData)
	}
}

func TestDateEvaluate_StringDate(t *testing.T) {
	// Quoted dates like `created: "2026-04-11"` remain strings — must still work.
	db := &DateBlock{Format: "2006-01-02"}
	findings := db.Evaluate("created", "2026-04-11", nil)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for string date, got %d", len(findings))
	}
}

func TestDateEvaluate_StringNotADate(t *testing.T) {
	// Quoted non-date string — should produce finding.
	db := &DateBlock{Format: "2006-01-02"}
	findings := db.Evaluate("created", "not-a-date", nil)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for bad string, got %d", len(findings))
	}
}
