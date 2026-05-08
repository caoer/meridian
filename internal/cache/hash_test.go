package cache

import (
	"testing"

	"github.com/caoer/meridian/internal/rules"
)

func TestFileHash_SameContent(t *testing.T) {
	h1 := FileHash([]byte("hello world"))
	h2 := FileHash([]byte("hello world"))
	if h1 != h2 {
		t.Fatalf("same content produced different hashes: %s vs %s", h1, h2)
	}
}

func TestFileHash_DifferentContent(t *testing.T) {
	h1 := FileHash([]byte("hello world"))
	h2 := FileHash([]byte("goodbye world"))
	if h1 == h2 {
		t.Fatal("different content produced same hash")
	}
}

func TestFileHash_Empty(t *testing.T) {
	h := FileHash([]byte{})
	if h == "" {
		t.Fatal("empty content should still produce a hash")
	}
}

func TestRuleHash_SameRule(t *testing.T) {
	r := rules.Rule{
		ID:       "test-rule",
		Check:    "frontmatter_exists",
		Message:  "missing {{.Field}}",
		Severity: rules.SeverityError,
		Params:   map[string]any{"field": "title"},
	}
	h1 := RuleHash(r)
	h2 := RuleHash(r)
	if h1 != h2 {
		t.Fatalf("same rule produced different hashes: %s vs %s", h1, h2)
	}
}

func TestRuleHash_DifferentParams(t *testing.T) {
	r1 := rules.Rule{
		ID:    "test-rule",
		Check: "frontmatter_exists",
		Params: map[string]any{"field": "title"},
	}
	r2 := rules.Rule{
		ID:    "test-rule",
		Check: "frontmatter_exists",
		Params: map[string]any{"field": "tags"},
	}
	if RuleHash(r1) == RuleHash(r2) {
		t.Fatal("rules with different params should have different hashes")
	}
}

func TestRuleHash_DifferentSeverity(t *testing.T) {
	r1 := rules.Rule{
		ID:       "test-rule",
		Check:    "frontmatter_exists",
		Severity: rules.SeverityError,
	}
	r2 := rules.Rule{
		ID:       "test-rule",
		Check:    "frontmatter_exists",
		Severity: rules.SeverityWarn,
	}
	if RuleHash(r1) == RuleHash(r2) {
		t.Fatal("rules with different severity should have different hashes")
	}
}

func TestRuleHash_DifferentID(t *testing.T) {
	r1 := rules.Rule{
		ID:    "rule-alpha",
		Check: "frontmatter_exists",
		Params: map[string]any{"field": "title"},
	}
	r2 := rules.Rule{
		ID:    "rule-beta",
		Check: "frontmatter_exists",
		Params: map[string]any{"field": "title"},
	}
	if RuleHash(r1) == RuleHash(r2) {
		t.Fatal("rules with different IDs should have different hashes")
	}
}

func TestRuleHash_DifferentOnFilter(t *testing.T) {
	r1 := rules.Rule{
		ID:    "test-rule",
		Check: "frontmatter_exists",
		On:    rules.ParseOnFilter([]string{"wiki/**"}),
	}
	r2 := rules.Rule{
		ID:    "test-rule",
		Check: "frontmatter_exists",
		On:    rules.ParseOnFilter([]string{"inbox/**"}),
	}
	if RuleHash(r1) == RuleHash(r2) {
		t.Fatal("rules with different On filters should have different hashes")
	}
}

func TestRuleHash_DifferentOnTags(t *testing.T) {
	r1 := rules.Rule{
		ID:    "test-rule",
		Check: "frontmatter_exists",
		On:    rules.ParseOnFilter([]string{"#domain/locus"}),
	}
	r2 := rules.Rule{
		ID:    "test-rule",
		Check: "frontmatter_exists",
		On:    rules.ParseOnFilter([]string{"#domain/mesh"}),
	}
	if RuleHash(r1) == RuleHash(r2) {
		t.Fatal("rules with different On tags should have different hashes")
	}
}

func TestRuleHash_OnOrderIndependent(t *testing.T) {
	r1 := rules.Rule{
		ID:    "test-rule",
		Check: "frontmatter_exists",
		On:    rules.ParseOnFilter([]string{"wiki/**", "inbox/**"}),
	}
	r2 := rules.Rule{
		ID:    "test-rule",
		Check: "frontmatter_exists",
		On:    rules.ParseOnFilter([]string{"inbox/**", "wiki/**"}),
	}
	if RuleHash(r1) != RuleHash(r2) {
		t.Fatal("On filter order should not affect hash (sorted internally)")
	}
}

func TestCombinedHash_Deterministic(t *testing.T) {
	fileH := FileHash([]byte("content"))
	ruleHashes := []string{"aaa", "ccc", "bbb"}

	h1 := CombinedHash(fileH, ruleHashes)
	// Reverse order — should produce same result (sorted internally).
	h2 := CombinedHash(fileH, []string{"bbb", "aaa", "ccc"})
	if h1 != h2 {
		t.Fatalf("combined hash not deterministic across rule hash order: %s vs %s", h1, h2)
	}
}

func TestCombinedHash_DifferentFile(t *testing.T) {
	ruleHashes := []string{"aaa"}
	h1 := CombinedHash(FileHash([]byte("a")), ruleHashes)
	h2 := CombinedHash(FileHash([]byte("b")), ruleHashes)
	if h1 == h2 {
		t.Fatal("different file content should produce different combined hashes")
	}
}

func TestCombinedHash_DifferentRules(t *testing.T) {
	fileH := FileHash([]byte("content"))
	h1 := CombinedHash(fileH, []string{"aaa"})
	h2 := CombinedHash(fileH, []string{"bbb"})
	if h1 == h2 {
		t.Fatal("different rule hashes should produce different combined hashes")
	}
}
