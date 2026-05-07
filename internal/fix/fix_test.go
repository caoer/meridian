package fix_test

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/fix"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/vfs"
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

// Fix #4: Deterministic ordering — two rules on same file produce consistent results.
func TestFixer_DeterministicOrdering(t *testing.T) {
	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("rule-b",
			testkit.Check("field-exists"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("tags"),
			testkit.MessageTemplate("missing {{.Field}}"),
		),
		testkit.Rule("rule-a",
			testkit.Check("field-exists"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("source"),
			testkit.MessageTemplate("missing {{.Field}}"),
		),
	}

	// Run 10 times — must produce identical results
	var firstFixed []fix.FixResult
	for i := 0; i < 10; i++ {
		testFS := testkit.Wiki(
			testkit.F("wiki/page.md", "---\ncreated: 2026-05-05\n---\n# Page\n"),
		)
		fixer := fix.New(eng, fix.All)
		report, err := fixer.Fix(testFS, ruleList, fix.Options{DryRun: true})
		if err != nil {
			t.Fatalf("iteration %d: Fix error: %v", i, err)
		}
		if i == 0 {
			firstFixed = report.Fixed
			continue
		}
		if len(report.Fixed) != len(firstFixed) {
			t.Fatalf("iteration %d: fixed count %d != %d", i, len(report.Fixed), len(firstFixed))
		}
		for j := range firstFixed {
			if report.Fixed[j].RuleID != firstFixed[j].RuleID {
				t.Errorf("iteration %d, result %d: ruleID %q != %q (nondeterministic)",
					i, j, report.Fixed[j].RuleID, firstFixed[j].RuleID)
			}
		}
	}
}

// Fix #5: fixFn error should appear in Unfixable, not be silently dropped.
func TestFixer_FixFnErrorReportsUnfixable(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ncreated: 2026-05-05\n---\n# Page\n"),
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

	// Register a fix that always errors
	registry := map[string]fix.FixFunc{
		"field-exists": func(content []byte, params map[string]any) (bool, []byte, []string, error) {
			return false, nil, nil, fmt.Errorf("simulated fix error")
		},
	}

	fixer := fix.New(eng, registry)
	report, err := fixer.Fix(fsys, ruleList, fix.Options{})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	// The finding should appear in Unfixable, not disappear
	if report.UnfixableCount == 0 {
		t.Error("expected unfixable entry for fix error, got 0")
	}
	foundReason := false
	for _, u := range report.Unfixable {
		if u.RuleID == "required-fields" && u.FilePath == "wiki/page.md" {
			foundReason = true
			if u.Reason == "" {
				t.Error("unfixable entry should have a reason")
			}
		}
	}
	if !foundReason {
		t.Error("expected unfixable entry for required-fields on wiki/page.md")
	}
}

// Fix #6: Write failure should move fixes to Unfixable, not return error.
func TestFixer_WriteFailureMovesToUnfixable(t *testing.T) {
	fsys := &failWriteFS{
		MemFS:    testkit.Wiki(
			testkit.F("wiki/good.md", "---\ncreated: 2026-05-05\n---\n# Good\n"),
			testkit.F("wiki/bad.md", "---\ncreated: 2026-05-05\n---\n# Bad\n"),
		),
		failPath: "wiki/bad.md",
	}
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
	report, err := fixer.Fix(fsys, ruleList, fix.Options{})
	if err != nil {
		t.Fatalf("Fix should not return error on write failure: %v", err)
	}

	// good.md should be fixed
	goodFixed := false
	for _, f := range report.Fixed {
		if f.FilePath == "wiki/good.md" {
			goodFixed = true
		}
	}
	if !goodFixed {
		t.Error("expected wiki/good.md in Fixed")
	}

	// bad.md should be in Unfixable
	badUnfixable := false
	for _, u := range report.Unfixable {
		if u.FilePath == "wiki/bad.md" {
			badUnfixable = true
			if u.Reason == "" {
				t.Error("unfixable entry should have write error reason")
			}
		}
	}
	if !badUnfixable {
		t.Error("expected wiki/bad.md in Unfixable after write failure")
	}
}

// failWriteFS wraps MemFS but fails WriteFile on a specific path.
type failWriteFS struct {
	*vfs.MemFS
	failPath string
}

func (f *failWriteFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if name == f.failPath {
		return fmt.Errorf("simulated write error for %s", name)
	}
	return f.MemFS.WriteFile(name, data, perm)
}

// Scope pre-filter: only files matching scope get fixed, others unchanged.
func TestFixer_ScopeFiltersPreventsWrites(t *testing.T) {
	originalB := "---\ncreated: 2026-05-05\n---\n# Other\n"
	fsys := testkit.Wiki(
		testkit.F("wiki/sub/a.md", "---\ncreated: 2026-05-05\n---\n# A\n"),
		testkit.F("wiki/other/b.md", originalB),
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
	report, err := fixer.Fix(fsys, ruleList, fix.Options{Scope: "wiki/sub/"})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	// File A in scope — should be fixed
	if report.FixedCount != 1 {
		t.Errorf("expected 1 fixed, got %d", report.FixedCount)
	}
	foundA := false
	for _, f := range report.Fixed {
		if f.FilePath == "wiki/sub/a.md" {
			foundA = true
		}
	}
	if !foundA {
		t.Error("expected wiki/sub/a.md in Fixed")
	}

	// File B out of scope — must NOT be modified
	dataB, err := fs.ReadFile(fsys, "wiki/other/b.md")
	if err != nil {
		t.Fatalf("read wiki/other/b.md: %v", err)
	}
	if string(dataB) != originalB {
		t.Errorf("out-of-scope file was modified!\nbefore: %q\nafter:  %q", originalB, string(dataB))
	}
}

// Scope with .md shorthand: "wiki/sub/page" matches "wiki/sub/page.md".
func TestFixer_ScopeMDShorthand(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/sub/page.md", "---\ncreated: 2026-05-05\n---\n# Page\n"),
		testkit.F("wiki/other/b.md", "---\ncreated: 2026-05-05\n---\n# Other\n"),
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
	report, err := fixer.Fix(fsys, ruleList, fix.Options{Scope: "wiki/sub/page"})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if report.FixedCount != 1 {
		t.Errorf("expected 1 fixed (page.md via shorthand), got %d", report.FixedCount)
	}
	for _, f := range report.Fixed {
		if f.FilePath != "wiki/sub/page.md" {
			t.Errorf("unexpected fixed file: %s", f.FilePath)
		}
	}
}

// Empty scope: all files processed (existing behavior preserved).
func TestFixer_EmptyScopeFixesAll(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/a.md", "---\ncreated: 2026-05-05\n---\n# A\n"),
		testkit.F("wiki/b.md", "---\ncreated: 2026-05-05\n---\n# B\n"),
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
	report, err := fixer.Fix(fsys, ruleList, fix.Options{})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if report.FixedCount != 2 {
		t.Errorf("expected 2 fixed with empty scope, got %d", report.FixedCount)
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
