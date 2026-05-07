package test

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/conflict"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/pkg/testkit"
)

// TestConflict_OverlapsAndClassify tests DetectOverlaps + Classify with
// 3 overlapping rules where only 1 pair is an actual conflict.
func TestConflict_OverlapsAndClassify(t *testing.T) {
	rr := []rules.Rule{
		testkit.Rule("pattern-heading",
			testkit.Check("pattern"),
			testkit.Severity("error"),
			testkit.On("wiki/**"),
			testkit.Param("target", "body"),
			testkit.Param("match", "^# .+"),
			testkit.MessageTemplate("Missing heading"),
		),
		testkit.Rule("pattern-heading-strict",
			testkit.Check("pattern"),
			testkit.Severity("error"),
			testkit.On("wiki/**"),
			testkit.Param("target", "body"),
			testkit.Param("match", "^# [A-Z].+"),
			testkit.MessageTemplate("Heading must start with uppercase"),
		),
		testkit.Rule("field-tags",
			testkit.Check("field-exists"),
			testkit.Severity("warn"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("tags"),
			testkit.MessageTemplate("Missing tags"),
		),
	}

	overlaps := conflict.DetectOverlaps(rr)
	if len(overlaps) < 3 {
		t.Fatalf("Expected at least 3 overlaps among 3 rules on wiki/**, got %d", len(overlaps))
	}

	conflicts := conflict.Classify(overlaps, rr)
	// Only pattern-heading vs pattern-heading-strict should conflict
	// (same check, same target, different match).
	if len(conflicts) != 1 {
		t.Fatalf("Expected 1 conflict, got %d", len(conflicts))
	}

	c := conflicts[0]
	if c.Overlap.RuleA != "pattern-heading" || c.Overlap.RuleB != "pattern-heading-strict" {
		t.Errorf("Unexpected conflict pair: %s vs %s", c.Overlap.RuleA, c.Overlap.RuleB)
	}

	if !strings.Contains(c.Detail, "body") {
		t.Errorf("Conflict detail should mention target 'body': %s", c.Detail)
	}
}

// TestConflict_CleanRuleSet verifies no conflicts/overlaps from disjoint rules.
func TestConflict_CleanRuleSet(t *testing.T) {
	rr := []rules.Rule{
		testkit.Rule("wiki-tags",
			testkit.Check("field-exists"),
			testkit.Severity("error"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("tags"),
			testkit.MessageTemplate("Missing tags"),
		),
		testkit.Rule("inbox-created",
			testkit.Check("field-exists"),
			testkit.Severity("warn"),
			testkit.On("inbox/**"),
			testkit.Frontmatter("created"),
			testkit.MessageTemplate("Missing created"),
		),
	}

	overlaps := conflict.DetectOverlaps(rr)
	if len(overlaps) != 0 {
		t.Errorf("Expected 0 overlaps for disjoint rules, got %d", len(overlaps))
	}

	conflicts := conflict.Classify(overlaps, rr)
	if len(conflicts) != 0 {
		t.Errorf("Expected 0 conflicts, got %d", len(conflicts))
	}
}

// TestConflict_ProfileFiltering verifies profile filtering before conflict check.
func TestConflict_ProfileFiltering(t *testing.T) {
	rr := []rules.Rule{
		testkit.Rule("pattern-a",
			testkit.Check("pattern"),
			testkit.Severity("error"),
			testkit.On("wiki/**"),
			testkit.Param("target", "body"),
			testkit.Param("match", "^# .+"),
			testkit.MessageTemplate("A"),
		),
		testkit.Rule("pattern-b",
			testkit.Check("pattern"),
			testkit.Severity("error"),
			testkit.On("wiki/**"),
			testkit.Param("target", "body"),
			testkit.Param("match", "^## .+"),
			testkit.MessageTemplate("B"),
		),
	}

	// Without profile — conflict exists.
	overlaps := conflict.DetectOverlaps(rr)
	conflicts := conflict.Classify(overlaps, rr)
	if len(conflicts) != 1 {
		t.Fatalf("Expected 1 conflict without profile filter, got %d", len(conflicts))
	}

	// With profile excluding pattern-b — no conflict.
	p := config.Profile{Exclude: []string{"pattern-b"}}
	filtered := p.Resolve(rr)
	if len(filtered) != 1 {
		t.Fatalf("Expected 1 rule after profile filter, got %d", len(filtered))
	}
	overlaps2 := conflict.DetectOverlaps(filtered)
	conflicts2 := conflict.Classify(overlaps2, filtered)
	if len(conflicts2) != 0 {
		t.Errorf("Expected 0 conflicts after profile filter, got %d", len(conflicts2))
	}
}

// TestConflict_CLIHandler tests the rules check handler produces correct JSON.
func TestConflict_CLIHandler(t *testing.T) {
	rr := []rules.Rule{
		testkit.Rule("pattern-heading",
			testkit.Check("pattern"),
			testkit.Severity("error"),
			testkit.On("wiki/**"),
			testkit.Param("target", "body"),
			testkit.Param("match", "^# .+"),
			testkit.MessageTemplate("Missing heading"),
		),
		testkit.Rule("pattern-heading-strict",
			testkit.Check("pattern"),
			testkit.Severity("error"),
			testkit.On("wiki/**"),
			testkit.Param("target", "body"),
			testkit.Param("match", "^# [A-Z].+"),
			testkit.MessageTemplate("Heading must start with uppercase"),
		),
		testkit.Rule("field-tags",
			testkit.Check("field-exists"),
			testkit.Severity("warn"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("tags"),
			testkit.MessageTemplate("Missing tags"),
		),
	}

	handler := cli.RulesCheckHandler(rr, nil)
	resp := handler(&cli.Request{Command: "rules check"})

	if resp.Version != "0.1" {
		t.Errorf("version = %q, want 0.1", resp.Version)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	// Marshal and re-parse data to verify JSON envelope.
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}

	var data cli.RulesCheckData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}

	if len(data.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(data.Conflicts))
	}
	if data.Conflicts[0].RuleA != "pattern-heading" {
		t.Errorf("conflict rule_a = %q, want pattern-heading", data.Conflicts[0].RuleA)
	}

	// 3 rules, 3 overlaps total. 1 is conflict, 2 are overlap-without-conflict.
	if len(data.OverlapsWithoutConflict) != 2 {
		t.Errorf("expected 2 overlaps without conflict, got %d", len(data.OverlapsWithoutConflict))
	}
}

// TestConflict_CLIBinary tests `md rules check` via the compiled binary.
func TestConflict_CLIBinary(t *testing.T) {
	bin := buildMD(t)
	dir := setupConflictWiki(t)

	cmd := exec.Command(bin, "rules", "check", `{"format":"json"}`)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rules check failed: %v\n%s", err, out)
	}

	var resp struct {
		Version string `json:"version"`
		Data    struct {
			Conflicts              []json.RawMessage `json:"conflicts"`
			OverlapsWithoutConflict []json.RawMessage `json:"overlaps_without_conflict"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if resp.Version != "0.1" {
		t.Errorf("version = %q, want 0.1", resp.Version)
	}
	// At least verify data structure present with arrays.
	if resp.Data.Conflicts == nil {
		t.Error("conflicts should be an array, got null")
	}
	if resp.Data.OverlapsWithoutConflict == nil {
		t.Error("overlaps_without_conflict should be an array, got null")
	}
}

// setupConflictWiki creates a temp wiki with overlapping rules.
func setupConflictWiki(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, dir+"/meridian.yaml", `
rule_packs:
  - path: rules/
scan:
  root: "."
  include:
    - "wiki/**"
`)

	mkdirAll(t, dir+"/rules")
	writeFile(t, dir+"/rules/pattern-heading.yaml", `
check: pattern
message: "Missing heading"
severity: error
on: "wiki/**"
target: body
match: "^# .+"
`)
	writeFile(t, dir+"/rules/pattern-heading-strict.yaml", `
check: pattern
message: "Heading must start with uppercase"
severity: error
on: "wiki/**"
target: body
match: "^# [A-Z].+"
`)
	writeFile(t, dir+"/rules/field-tags.yaml", `
check: field-exists
message: "Missing tags"
severity: warn
on: "wiki/**"
frontmatter: [tags]
`)

	mkdirAll(t, dir+"/wiki")
	writeFile(t, dir+"/wiki/test.md", `---
tags: [test]
created: 2026-05-05
---
# Test
`)
	return dir
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
