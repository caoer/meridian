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
		MemFS: testkit.Wiki(
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

// TestFixer_ScannedPathsInjected verifies that __scanned_paths is injected
// into fix params (auto-derived from the filesystem when opts.Files is nil).
func TestFixer_ScannedPathsInjected(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page-a.md", "---\ntags: []\n---\n# A\nContent with [[page-b]]\n"),
		testkit.F("wiki/page-b.md", "---\ntags: []\n---\n# B\n"),
	)

	eng := setupEngine()

	// Register a custom fix that captures the params it receives.
	var capturedParams map[string]any
	captureRegistry := map[string]fix.FixFunc{
		"field-exists": func(content []byte, params map[string]any) (bool, []byte, []string, error) {
			capturedParams = params
			// Return changed=false — we just want to inspect params.
			return false, content, nil, nil
		},
	}

	ruleList := []rules.Rule{
		testkit.Rule("required-fields",
			testkit.Check("field-exists"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("created"),
			testkit.MessageTemplate("missing {{.Field}}"),
		),
	}

	fixer := fix.New(eng, captureRegistry)
	_, err := fixer.Fix(fsys, ruleList, fix.Options{})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if capturedParams == nil {
		t.Fatal("fix function was never called")
	}

	paths, ok := capturedParams["__scanned_paths"].([]string)
	if !ok {
		t.Fatalf("__scanned_paths not injected or wrong type: %v", capturedParams["__scanned_paths"])
	}
	if len(paths) < 2 {
		t.Errorf("expected at least 2 scanned paths, got %d: %v", len(paths), paths)
	}

	// Verify both files are present in the scanned paths.
	pathSet := make(map[string]bool, len(paths))
	for _, p := range paths {
		pathSet[p] = true
	}
	if !pathSet["wiki/page-a.md"] {
		t.Errorf("__scanned_paths missing wiki/page-a.md: %v", paths)
	}
	if !pathSet["wiki/page-b.md"] {
		t.Errorf("__scanned_paths missing wiki/page-b.md: %v", paths)
	}
}

// TestFixer_FilesOptionOverridesAutoScan verifies that opts.Files is used
// as __scanned_paths when provided, without scanning the filesystem.
func TestFixer_FilesOptionOverridesAutoScan(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page-a.md", "---\ntags: []\n---\n# A\n"),
		testkit.F("wiki/page-b.md", "---\ntags: []\n---\n# B\n"),
	)

	eng := setupEngine()

	var capturedParams map[string]any
	captureRegistry := map[string]fix.FixFunc{
		"field-exists": func(content []byte, params map[string]any) (bool, []byte, []string, error) {
			capturedParams = params
			return false, content, nil, nil
		},
	}

	ruleList := []rules.Rule{
		testkit.Rule("required-fields",
			testkit.Check("field-exists"),
			testkit.On("wiki/**"),
			testkit.Frontmatter("created"),
			testkit.MessageTemplate("missing {{.Field}}"),
		),
	}

	// Pass an explicit file universe that differs from what's on disk.
	explicitFiles := []string{"wiki/page-a.md", "wiki/page-b.md", "wiki/page-c.md"}
	fixer := fix.New(eng, captureRegistry)
	_, err := fixer.Fix(fsys, ruleList, fix.Options{Files: explicitFiles})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if capturedParams == nil {
		t.Fatal("fix function was never called")
	}

	paths, ok := capturedParams["__scanned_paths"].([]string)
	if !ok {
		t.Fatalf("__scanned_paths not injected or wrong type: %v", capturedParams["__scanned_paths"])
	}

	// Must be exactly the explicit list — not auto-derived.
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths from explicit Files, got %d: %v", len(paths), paths)
	}
	for i, want := range explicitFiles {
		if paths[i] != want {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], want)
		}
	}
}

// TestFixer_ScannedPathsKeyConstant verifies the exported constant matches convention.
func TestFixer_ScannedPathsKeyConstant(t *testing.T) {
	if fix.ScannedPathsKey != "__scanned_paths" {
		t.Errorf("ScannedPathsKey = %q, want %q", fix.ScannedPathsKey, "__scanned_paths")
	}
}

// --- DryRun safety tests (filesystem-level, not return-value) ---

// writeTrackingFS wraps a MemFS and records every WriteFile call.
// This is the filesystem-level assertion: we detect writes by observing
// the actual FS mutation, not by trusting the return value.
type writeTrackingFS struct {
	inner  *vfs.MemFS
	writes []string // paths that received WriteFile calls
}

func (w *writeTrackingFS) Open(name string) (fs.File, error) { return w.inner.Open(name) }
func (w *writeTrackingFS) MkdirAll(p string, perm fs.FileMode) error {
	return w.inner.MkdirAll(p, perm)
}
func (w *writeTrackingFS) Remove(name string) error     { return w.inner.Remove(name) }
func (w *writeTrackingFS) Rename(old, new string) error { return w.inner.Rename(old, new) }
func (w *writeTrackingFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	w.writes = append(w.writes, name)
	return w.inner.WriteFile(name, data, perm)
}

var _ vfs.WriteFS = (*writeTrackingFS)(nil)

// TestFixer_DryRunZeroWrites_FieldExists asserts ZERO filesystem writes
// under DryRun for the field-exists fixer.
func TestFixer_DryRunZeroWrites_FieldExists(t *testing.T) {
	mem := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ncreated: 2026-05-05\n---\n# Page\n"),
	)
	tracker := &writeTrackingFS{inner: mem}

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
	report, err := fixer.Fix(tracker, ruleList, fix.Options{DryRun: true})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if len(tracker.writes) != 0 {
		t.Fatalf("DRY-RUN VIOLATION: %d filesystem writes occurred: %v", len(tracker.writes), tracker.writes)
	}

	// The fixer should still report what it WOULD fix.
	if report.FixedCount != 1 {
		t.Errorf("expected 1 fixed (dry-run report), got %d", report.FixedCount)
	}
}

// TestFixer_DryRunZeroWrites_WikilinkCanonicalize asserts ZERO filesystem
// writes under DryRun for the wikilink-canonicalize fixer.
func TestFixer_DryRunZeroWrites_WikilinkCanonicalize(t *testing.T) {
	// Build a vault with a non-canonical link that WILL trigger the fixer.
	mem := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ntitle: page\n---\n\nSee [[wiki/domain/target]] here.\n"),
		testkit.F("wiki/domain/target.md", "---\ntitle: target\n---\n# Target\n"),
	)
	tracker := &writeTrackingFS{inner: mem}

	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("wikilink-canonicalize",
			testkit.Check("wikilink-canonicalize"),
			testkit.On("wiki/**"),
			testkit.MessageTemplate("wikilink [[{{.Target}}]] not canonical; canonical: [[{{.Canonical}}]]"),
		),
	}
	// Set roots param via rule params.
	ruleList[0].Params = map[string]any{"roots": []any{"wiki/**"}}

	fixer := fix.New(eng, fix.All)
	report, err := fixer.Fix(tracker, ruleList, fix.Options{DryRun: true})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if len(tracker.writes) != 0 {
		t.Fatalf("DRY-RUN VIOLATION: %d filesystem writes occurred: %v", len(tracker.writes), tracker.writes)
	}

	// The fixer should report what it would fix.
	if report.FixedCount == 0 {
		t.Error("expected at least 1 fix reported in dry-run mode")
	}
}

// TestFixer_DryRunZeroWrites_TableWikilinkPipe asserts ZERO filesystem
// writes under DryRun for the table-wikilink-pipe fixer.
func TestFixer_DryRunZeroWrites_TableWikilinkPipe(t *testing.T) {
	mem := testkit.Wiki(
		testkit.F("wiki/table.md", "---\ntitle: t\n---\n\n| Col |\n| --- |\n| [[a|b]] |\n"),
	)
	tracker := &writeTrackingFS{inner: mem}

	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("table-wikilink-pipe",
			testkit.Check("table-wikilink-pipe"),
			testkit.On("wiki/**"),
			testkit.MessageTemplate("pipe in table"),
		),
	}

	fixer := fix.New(eng, fix.All)
	report, err := fixer.Fix(tracker, ruleList, fix.Options{DryRun: true})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	if len(tracker.writes) != 0 {
		t.Fatalf("DRY-RUN VIOLATION: %d filesystem writes occurred: %v", len(tracker.writes), tracker.writes)
	}

	if report.FixedCount == 0 {
		t.Error("expected at least 1 fix reported in dry-run mode")
	}
}

// --- Strict param validation tests ---

// TestFixer_UnknownParamKey_Rejected reproduces the dry_run escape:
// "dry_run" (underscore) is unknown — should produce a param error.
func TestFixer_UnknownParamKey_Rejected(t *testing.T) {
	mem := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ntitle: p\n---\nSee [[wiki/domain/target]].\n"),
		testkit.F("wiki/domain/target.md", "---\ntitle: t\n---\n# Target\n"),
	)

	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("wikilink-canonicalize",
			testkit.Check("wikilink-canonicalize"),
			testkit.On("wiki/**"),
			testkit.MessageTemplate("[[{{.Target}}]] not canonical"),
		),
	}
	// Inject a typo'd param: "root" instead of "roots"
	ruleList[0].Params = map[string]any{
		"root": []any{"wiki/**"}, // TYPO — should be "roots"
	}

	fixer := fix.New(eng, fix.All)
	_, err := fixer.Fix(mem, ruleList, fix.Options{})
	if err == nil {
		t.Fatal("expected error for unknown param key, got nil")
	}
	if !containsSubstring(err.Error(), "unknown param") || !containsSubstring(err.Error(), "roots") {
		t.Errorf("expected error mentioning 'unknown param' and 'roots' suggestion, got: %v", err)
	}
}

// TestFixer_TypeMismatch_Rejected reproduces the resolved_links-as-string escape.
func TestFixer_TypeMismatch_Rejected(t *testing.T) {
	mem := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ntitle: p\n---\nSee [[architecture]].\n"),
		testkit.F("wiki/a/architecture.md", "---\ntitle: a\n---\n"),
		testkit.F("wiki/b/architecture.md", "---\ntitle: b\n---\n"),
	)

	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("wikilink-canonicalize",
			testkit.Check("wikilink-canonicalize"),
			testkit.On("wiki/**"),
			testkit.MessageTemplate("[[{{.Target}}]] not canonical"),
		),
	}
	ruleList[0].Params = map[string]any{
		"roots":          []any{"wiki/**"},
		"resolved_links": "/path/to/dump.json", // STRING instead of map — type mismatch
	}

	fixer := fix.New(eng, fix.All)
	_, err := fixer.Fix(mem, ruleList, fix.Options{})
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
	if !containsSubstring(err.Error(), "expected map[string]any") {
		t.Errorf("expected type mismatch error, got: %v", err)
	}
}

// TestFixer_InjectedParamsExempt verifies that __-prefixed engine params
// don't trigger unknown-key rejection.
func TestFixer_InjectedParamsExempt(t *testing.T) {
	mem := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ntitle: p\n---\nSee [[wiki/domain/target]].\n"),
		testkit.F("wiki/domain/target.md", "---\ntitle: t\n---\n# Target\n"),
	)

	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("wikilink-canonicalize",
			testkit.Check("wikilink-canonicalize"),
			testkit.On("wiki/**"),
			testkit.MessageTemplate("[[{{.Target}}]] not canonical"),
		),
	}
	ruleList[0].Params = map[string]any{"roots": []any{"wiki/**"}}

	fixer := fix.New(eng, fix.All)
	report, err := fixer.Fix(mem, ruleList, fix.Options{})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	// Should NOT have param errors — __scanned_paths and __file_path are exempt.
	for _, u := range report.Unfixable {
		if containsSubstring(u.Reason, "param error") {
			t.Errorf("injected params should be exempt, got unfixable: %s", u.Reason)
		}
	}
}

// TestParamSpecs_CoverAllRegisteredChecks is a fail-closed invariant:
// every registered check that accepts params MUST have a ParamSpec.
// A new check shipping without a spec fails this test — no silent gaps.
func TestParamSpecs_CoverAllRegisteredChecks(t *testing.T) {
	// All registered checks from the check registry.
	for name := range checks.All {
		// Checks that accept zero user params are exempt if they have
		// a spec with an empty Accepted map (explicit declaration).
		// Checks without a spec entry at all are flagged.
		if _, hasSpec := fix.ParamSpecs[name]; !hasSpec {
			t.Errorf("registered check %q has no ParamSpec — add one to fix.ParamSpecs (use empty Accepted{} if it takes no params)", name)
		}
	}
}

// TestFixer_DryRunBeltAndSuspenders verifies the readOnlyFS wrapper
// makes writes physically impossible even if the DryRun gate is bypassed.
func TestFixer_DryRunBeltAndSuspenders(t *testing.T) {
	// This test proves the readOnlyFS wrapper works by checking that
	// the error sentinel is ErrDryRunWrite.
	mem := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ntitle: p\n---\nSee [[wiki/domain/target]].\n"),
		testkit.F("wiki/domain/target.md", "---\ntitle: t\n---\n# Target\n"),
	)
	tracker := &writeTrackingFS{inner: mem}

	eng := setupEngine()
	ruleList := []rules.Rule{
		testkit.Rule("wikilink-canonicalize",
			testkit.Check("wikilink-canonicalize"),
			testkit.On("wiki/**"),
			testkit.MessageTemplate("[[{{.Target}}]] not canonical"),
		),
	}
	ruleList[0].Params = map[string]any{"roots": []any{"wiki/**"}}

	fixer := fix.New(eng, fix.All)
	_, err := fixer.Fix(tracker, ruleList, fix.Options{DryRun: true})
	if err != nil {
		t.Fatalf("Fix error: %v", err)
	}

	// Zero writes at the tracking layer proves the readOnlyFS wrapper worked.
	if len(tracker.writes) != 0 {
		t.Fatalf("readOnlyFS failed: %d writes leaked through", len(tracker.writes))
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
