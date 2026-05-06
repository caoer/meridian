package cli

import (
	"encoding/json"
	"testing"
)

func TestResponse_ExitCode_Clean(t *testing.T) {
	r := &Response{Version: ResponseVersion}

	got := r.ExitCode()

	if got != 0 {
		t.Fatalf("expected exit code 0, got %d", got)
	}
}

func TestResponse_ExitCode_ErrorFindings(t *testing.T) {
	r := &Response{
		Version: ResponseVersion,
		Findings: []Finding{
			{RuleID: "r1", Severity: "error", FilePath: "a.md", Message: "bad"},
		},
	}

	got := r.ExitCode()

	if got != 1 {
		t.Fatalf("expected exit code 1, got %d", got)
	}
}

func TestResponse_ExitCode_WarnOnly(t *testing.T) {
	r := &Response{
		Version: ResponseVersion,
		Findings: []Finding{
			{RuleID: "r1", Severity: "warning", FilePath: "a.md", Message: "meh"},
			{RuleID: "r2", Severity: "warning", FilePath: "b.md", Message: "also meh"},
		},
	}

	got := r.ExitCode()

	if got != 0 {
		t.Fatalf("expected exit code 0 for warn-only findings, got %d", got)
	}
}

func TestResponse_ExitCode_ToolFailure(t *testing.T) {
	r := &Response{
		Version: ResponseVersion,
		Findings: []Finding{
			{RuleID: "r1", Severity: "error", FilePath: "a.md", Message: "bad"},
		},
		Error: &ErrorDetail{Code: ErrNoConfig, Message: "missing config"},
	}

	got := r.ExitCode()

	if got != 2 {
		t.Fatalf("expected exit code 2 (tool failure takes precedence), got %d", got)
	}
}

func TestResponse_JSON_Serialization(t *testing.T) {
	r := &Response{
		Version: ResponseVersion,
		Findings: []Finding{
			{RuleID: "r1", Severity: "error", FilePath: "a.md", Line: 1, Column: 1, Message: "bad"},
		},
		Stats: &Stats{
			FilesScanned:  10,
			FilesSkipped:  2,
			RulesApplied:  3,
			FindingsCount: 1,
			DurationMs:    42,
		},
		Warnings: []Warning{
			{Code: "SLOW", Message: "took a while"},
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	for _, key := range []string{"version", "findings", "stats", "warnings"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON output", key)
		}
	}

	// error and data should be omitted when nil/empty
	if _, ok := m["error"]; ok {
		t.Error("error key should be omitted when nil")
	}
	if _, ok := m["data"]; ok {
		t.Error("data key should be omitted when nil")
	}
}

func TestResponse_JSON_ErrorResponse(t *testing.T) {
	r := ErrorResponse(ErrNoConfig, "no meridian.yaml found")

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var out Response
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if out.Version != ResponseVersion {
		t.Errorf("version = %q, want %q", out.Version, ResponseVersion)
	}
	if out.Error == nil {
		t.Fatal("error should not be nil")
	}
	if out.Error.Code != ErrNoConfig {
		t.Errorf("error code = %q, want %q", out.Error.Code, ErrNoConfig)
	}
	if out.Error.Message != "no meridian.yaml found" {
		t.Errorf("error message = %q, want %q", out.Error.Message, "no meridian.yaml found")
	}
	if out.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", out.ExitCode())
	}
}

func TestFinding_JSON(t *testing.T) {
	f := Finding{
		RuleID:   "title-required",
		Severity: "error",
		FilePath: "docs/intro.md",
		Line:     5,
		Column:   3,
		Message:  "missing title in frontmatter",
	}

	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	for _, key := range []string{"rule_id", "severity", "file_path", "line", "column", "message"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in Finding JSON", key)
		}
	}

	// Round-trip check
	var out Finding
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v", err)
	}
	if out != f {
		t.Errorf("round-trip mismatch: got %+v, want %+v", out, f)
	}
}

func TestErrorResponseWithHint(t *testing.T) {
	r := ErrorResponseWithHint(ErrInvalidConfig, "bad yaml", "check indentation on line 12")

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var out Response
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if out.Error == nil {
		t.Fatal("error should not be nil")
	}
	if out.Error.Code != ErrInvalidConfig {
		t.Errorf("code = %q, want %q", out.Error.Code, ErrInvalidConfig)
	}
	if out.Error.Hint != "check indentation on line 12" {
		t.Errorf("hint = %q, want %q", out.Error.Hint, "check indentation on line 12")
	}
	if out.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", out.ExitCode())
	}
}
