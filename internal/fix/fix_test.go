package fix_test

import (
	"io/fs"
	"testing"

	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/fix"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/pkg/testkit"
)

func setupEngine() *engine.Engine {
	eng := engine.New()
	for name, fn := range checks.All {
		eng.RegisterCheck(name, fn)
	}
	return eng
}

func TestFixer_FixMissingField(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ncreated: 2026-05-05\n---\n# Page\n"),
	)
	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("required-fields",
			testkit.Check("field-exists"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("tags", "created"),
			testkit.MessageTemplate("missing {{.Field}}"),
		),
	}

	fixer := fix.New(eng, fix.All)
	report, err := fixer.Fix(fsys, ruleList, fix.Options{})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if report.FixedCount != 1 {
		t.Errorf("expected 1 fixed, got %d", report.FixedCount)
	}
	if report.UnfixableCount != 0 {
		t.Errorf("expected 0 unfixable, got %d", report.UnfixableCount)
	}

	// Verify file was written
	data, err := fs.ReadFile(fsys, "wiki/page.md")
	if err != nil {
		t.Fatalf("read fixed file: %v", err)
	}
	got := string(data)
	if !containsSubstring(got, "tags: []") {
		t.Errorf("fixed file missing 'tags: []':\n%s", got)
	}

	// Verify re-check: engine should find zero findings for this rule
	reFindings := eng.Run(fsys, ruleList)
	if len(reFindings) != 0 {
		t.Errorf("expected 0 findings after fix, got %d", len(reFindings))
	}
}

func TestFixer_DryRunDoesNotWrite(t *testing.T) {
	original := "---\ncreated: 2026-05-05\n---\n# Page\n"
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", original),
	)
	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("required-fields",
			testkit.Check("field-exists"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("tags"),
			testkit.MessageTemplate("missing {{.Field}}"),
		),
	}

	fixer := fix.New(eng, fix.All)
	report, err := fixer.Fix(fsys, ruleList, fix.Options{DryRun: true})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if report.FixedCount != 1 {
		t.Errorf("expected 1 fixed in dry-run, got %d", report.FixedCount)
	}

	// File must NOT be modified
	data, err := fs.ReadFile(fsys, "wiki/page.md")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != original {
		t.Errorf("dry-run modified file!\nbefore: %q\nafter:  %q", original, string(data))
	}
}

func TestFixer_UnfixableRuleReportsSkip(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ntags: [bad-tag]\n---\n# Page\n"),
	)
	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("tag-prefix",
			testkit.Check("tag-format"),
			testkit.On("wiki/**"),
			testkit.Param("prefixes", []any{"domain"}),
			testkit.MessageTemplate("tag {{.Tag}} missing prefix"),
		),
	}

	fixer := fix.New(eng, fix.All)
	report, err := fixer.Fix(fsys, ruleList, fix.Options{})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if report.UnfixableCount == 0 {
		t.Error("expected unfixable findings for tag-format")
	}
	if report.FixedCount != 0 {
		t.Errorf("expected 0 fixed, got %d", report.FixedCount)
	}
}

func TestFixer_MixedFixableAndUnfixable(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ntags: [bad]\n---\n# Page\n"),
	)
	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("required-fields",
			testkit.Check("field-exists"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("created"),
			testkit.MessageTemplate("missing {{.Field}}"),
		),
		testkit.Rule("tag-prefix",
			testkit.Check("tag-format"),
			testkit.On("wiki/**"),
			testkit.Param("prefixes", []any{"domain"}),
			testkit.MessageTemplate("tag {{.Tag}} missing prefix"),
		),
	}

	fixer := fix.New(eng, fix.All)
	report, err := fixer.Fix(fsys, ruleList, fix.Options{})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if report.FixedCount < 1 {
		t.Errorf("expected >=1 fixed, got %d", report.FixedCount)
	}
	if report.UnfixableCount < 1 {
		t.Errorf("expected >=1 unfixable, got %d", report.UnfixableCount)
	}
}

func TestFixer_NoFindings(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ntags: []\ncreated: 2026-05-05\n---\n# Page\n"),
	)
	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("required-fields",
			testkit.Check("field-exists"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("tags", "created"),
			testkit.MessageTemplate("missing {{.Field}}"),
		),
	}

	fixer := fix.New(eng, fix.All)
	report, err := fixer.Fix(fsys, ruleList, fix.Options{})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if report.FixedCount != 0 || report.UnfixableCount != 0 {
		t.Errorf("expected empty report, got fixed=%d unfixable=%d",
			report.FixedCount, report.UnfixableCount)
	}
}

func TestFixer_FixThenRecheckPasses(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/a.md", "---\ntitle: A\n---\n# A\n"),
		testkit.F("wiki/b.md", "# B no frontmatter\n"),
	)
	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("required-fields",
			testkit.Check("field-exists"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("tags"),
			testkit.MessageTemplate("missing {{.Field}}"),
		),
	}

	// Before fix: should have findings
	before := eng.Run(fsys, ruleList)
	if len(before) == 0 {
		t.Fatal("expected findings before fix")
	}

	fixer := fix.New(eng, fix.All)
	report, err := fixer.Fix(fsys, ruleList, fix.Options{})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}
	if report.FixedCount != 2 {
		t.Errorf("expected 2 fixed, got %d", report.FixedCount)
	}

	// After fix: engine re-check should produce zero findings
	after := eng.Run(fsys, ruleList)
	if len(after) != 0 {
		t.Errorf("expected 0 findings after fix, got %d", len(after))
		for _, f := range after {
			t.Logf("  %s: %s (%s)", f.FilePath, f.Message, f.RuleID)
		}
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && searchSubstring(s, sub)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
