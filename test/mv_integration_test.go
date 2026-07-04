package test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/mv"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/vfs"
	"github.com/caoer/meridian/pkg/testkit"
)

// buildWiki creates a cross-linked wiki with 5 files across 3 domains.
func buildWiki() *vfs.MemFS {
	return testkit.Wiki(
		testkit.F("wiki/locus/overview.md", `---
tags: [domain/locus]
---
# Overview
See [[design-doc]] and [[api-ref]] for details.
`),
		testkit.F("wiki/locus/design-doc.md", `---
tags: [domain/locus]
---
# Design Doc
Back to [[overview]].
`),
		testkit.F("wiki/infra/deploy-guide.md", `---
tags: [domain/infra]
deploy-target: production
---
# Deploy Guide
Based on [[design-doc]].
`),
		testkit.F("wiki/shared/api-ref.md", `---
tags: [domain/shared]
---
# API Reference
See [[overview]] for context.
`),
		testkit.F("wiki/shared/glossary.md", `---
tags: [domain/shared]
---
# Glossary
No wikilinks here.
`),
	)
}

// fieldExistsCheck is a minimal check that verifies frontmatter fields exist.
func fieldExistsCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	fields, _ := params["frontmatter"].([]any)
	var out []engine.RawFinding
	for _, f := range fields {
		name, _ := f.(string)
		if _, ok := doc.Frontmatter[name]; !ok {
			out = append(out, engine.RawFinding{
				TemplateData: map[string]string{"Field": name},
			})
		}
	}
	return out
}

func TestMvE2E_MoveWithSameStem(t *testing.T) {
	m := buildWiki()

	eng := engine.New()
	result, err := mv.Move(m, "wiki/locus/design-doc.md", "wiki/infra/design-doc.md", eng, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	// Source gone
	if _, err := fs.Stat(m, "wiki/locus/design-doc.md"); err == nil {
		t.Error("source still exists")
	}
	// Dest exists
	if _, err := fs.Stat(m, "wiki/infra/design-doc.md"); err != nil {
		t.Fatal("dest missing")
	}

	// Stem unchanged (design-doc → design-doc) — no link rewrites needed
	if len(result.LinkUpdates) != 0 {
		t.Errorf("link_updates = %d, want 0 (same stem)", len(result.LinkUpdates))
	}

	// overview.md [[design-doc]] still valid (stem unchanged)
	data, _ := fs.ReadFile(m, "wiki/locus/overview.md")
	if !strings.Contains(string(data), "[[design-doc]]") {
		t.Error("overview.md lost [[design-doc]] link")
	}

	// deploy-guide.md [[design-doc]] still valid
	data, _ = fs.ReadFile(m, "wiki/infra/deploy-guide.md")
	if !strings.Contains(string(data), "[[design-doc]]") {
		t.Error("deploy-guide.md lost [[design-doc]] link")
	}

	if result.FilesCount != 1 {
		t.Errorf("files_count = %d, want 1", result.FilesCount)
	}
}

func TestMvE2E_MoveWithStemChange(t *testing.T) {
	m := buildWiki()

	eng := engine.New()
	result, err := mv.Move(m, "wiki/locus/design-doc.md", "wiki/infra/arch-doc.md", eng, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	// All [[design-doc]] should become [[arch-doc]]
	if len(result.LinkUpdates) == 0 {
		t.Fatal("expected link updates for stem change")
	}

	// Check overview.md rewritten
	data, _ := fs.ReadFile(m, "wiki/locus/overview.md")
	body := string(data)
	if strings.Contains(body, "[[design-doc]]") {
		t.Error("overview.md still has [[design-doc]]")
	}
	if !strings.Contains(body, "[[arch-doc]]") {
		t.Error("overview.md missing [[arch-doc]]")
	}

	// Check deploy-guide.md rewritten
	data, _ = fs.ReadFile(m, "wiki/infra/deploy-guide.md")
	body = string(data)
	if strings.Contains(body, "[[design-doc]]") {
		t.Error("deploy-guide.md still has [[design-doc]]")
	}
	if !strings.Contains(body, "[[arch-doc]]") {
		t.Error("deploy-guide.md missing [[arch-doc]]")
	}

	// Count total rewrites across files
	totalRewrites := 0
	for _, lu := range result.LinkUpdates {
		totalRewrites += lu.Count
		if lu.OldLink != "wiki/locus/design-doc" {
			t.Errorf("old_link = %q, want wiki/locus/design-doc", lu.OldLink)
		}
		if lu.NewLink != "wiki/infra/arch-doc" {
			t.Errorf("new_link = %q, want wiki/infra/arch-doc", lu.NewLink)
		}
	}
	if totalRewrites < 2 {
		t.Errorf("total rewrites = %d, want >= 2", totalRewrites)
	}
}

func TestMvE2E_TagMismatchDetection(t *testing.T) {
	m := buildWiki()

	eng := engine.New()
	result, err := mv.Move(m, "wiki/locus/design-doc.md", "wiki/infra/design-doc.md", eng, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	// File had domain/locus but now lives in wiki/infra/
	if len(result.TagSuggestions) != 1 {
		t.Fatalf("tag suggestions = %d, want 1", len(result.TagSuggestions))
	}
	ts := result.TagSuggestions[0]
	if ts.OldTag != "domain/locus" {
		t.Errorf("old_tag = %q, want domain/locus", ts.OldTag)
	}
	if ts.NewTag != "domain/infra" {
		t.Errorf("new_tag = %q, want domain/infra", ts.NewTag)
	}
	if ts.File != "wiki/infra/design-doc.md" {
		t.Errorf("file = %q, want wiki/infra/design-doc.md", ts.File)
	}
}

func TestMvE2E_PostMoveRelint(t *testing.T) {
	m := buildWiki()

	eng := engine.New()
	eng.RegisterCheck("field-exists", fieldExistsCheck)

	rl := []rules.Rule{
		testkit.Rule("require-deploy-target",
			testkit.Check("field-exists"),
			testkit.Severity("warn"),
			testkit.On("wiki/infra/**"),
			testkit.MessageTemplate("Missing required field: {{.Field}}"),
			testkit.Frontmatter("deploy-target"),
		),
	}

	// design-doc.md has no deploy-target field. After moving to wiki/infra/,
	// it now matches the wiki/infra/** rule.
	result, err := mv.Move(m, "wiki/locus/design-doc.md", "wiki/infra/design-doc.md", eng, rl, false)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.RelintFindings) == 0 {
		t.Fatal("expected relint findings for missing deploy-target")
	}

	found := false
	for _, f := range result.RelintFindings {
		if f.FilePath == "wiki/infra/design-doc.md" && strings.Contains(f.Message, "deploy-target") {
			found = true
		}
	}
	if !found {
		t.Errorf("relint findings missing deploy-target check for wiki/infra/design-doc.md, got: %+v", result.RelintFindings)
	}
}

func TestMvE2E_DryRunNoChanges(t *testing.T) {
	m := buildWiki()

	eng := engine.New()
	eng.RegisterCheck("field-exists", fieldExistsCheck)

	rl := []rules.Rule{
		testkit.Rule("require-deploy-target",
			testkit.Check("field-exists"),
			testkit.Severity("warn"),
			testkit.On("wiki/infra/**"),
			testkit.MessageTemplate("Missing required field: {{.Field}}"),
			testkit.Frontmatter("deploy-target"),
		),
	}

	// Save original content for comparison
	origOverview, _ := fs.ReadFile(m, "wiki/locus/overview.md")
	origDesign, _ := fs.ReadFile(m, "wiki/locus/design-doc.md")

	result, err := mv.Move(m, "wiki/locus/design-doc.md", "wiki/infra/arch-doc.md", eng, rl, true)
	if err != nil {
		t.Fatal(err)
	}

	// Result should have meaningful data
	if result.FilesCount != 1 {
		t.Errorf("files_count = %d, want 1", result.FilesCount)
	}

	// Link updates computed (stem changes)
	if len(result.LinkUpdates) == 0 {
		t.Error("dry run should compute link updates")
	}

	// Tag suggestions computed
	if len(result.TagSuggestions) == 0 {
		t.Error("dry run should compute tag suggestions")
	}

	// Filesystem unchanged
	if _, err := fs.Stat(m, "wiki/locus/design-doc.md"); err != nil {
		t.Error("dry run removed source!")
	}
	if _, err := fs.Stat(m, "wiki/infra/arch-doc.md"); err == nil {
		t.Error("dry run created dest!")
	}

	// Content unchanged
	nowOverview, _ := fs.ReadFile(m, "wiki/locus/overview.md")
	if string(nowOverview) != string(origOverview) {
		t.Error("dry run modified overview.md!")
	}
	nowDesign, _ := fs.ReadFile(m, "wiki/locus/design-doc.md")
	if string(nowDesign) != string(origDesign) {
		t.Error("dry run modified design-doc.md!")
	}
}

func TestMvE2E_ErrorMissingSource(t *testing.T) {
	m := buildWiki()

	eng := engine.New()
	_, err := mv.Move(m, "wiki/nonexistent.md", "wiki/dest.md", eng, nil, false)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestMvE2E_ErrorDestExists(t *testing.T) {
	m := buildWiki()

	eng := engine.New()
	_, err := mv.Move(m, "wiki/locus/design-doc.md", "wiki/shared/api-ref.md", eng, nil, false)
	if err == nil {
		t.Fatal("expected error for existing dest")
	}

	// Source should be untouched since move failed
	if _, err := fs.Stat(m, "wiki/locus/design-doc.md"); err != nil {
		t.Error("source was modified despite error")
	}
}

// --- P2e: anchored + path-qualified link rewrite tests ---

func TestMvE2E_AnchoredLinksRewritten(t *testing.T) {
	m := testkit.Wiki(
		testkit.F("wiki/locus/target.md", "---\n---\n# Target page\n"),
		testkit.F("wiki/refs.md", "---\n---\nSee [[target#section]] and [[target#other|display]].\n"),
	)

	eng := engine.New()
	result, err := mv.Move(m, "wiki/locus/target.md", "wiki/infra/renamed.md", eng, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	// Both anchored links should be rewritten
	data, _ := fs.ReadFile(m, "wiki/refs.md")
	body := string(data)
	if !strings.Contains(body, "[[renamed#section]]") {
		t.Errorf("missing [[renamed#section]], got: %s", body)
	}
	if !strings.Contains(body, "[[renamed#other|display]]") {
		t.Errorf("missing [[renamed#other|display]], got: %s", body)
	}
	if strings.Contains(body, "[[target#") {
		t.Errorf("old anchored links still present: %s", body)
	}

	// Should report 2 rewrites
	total := 0
	for _, lu := range result.LinkUpdates {
		total += lu.Count
	}
	if total != 2 {
		t.Errorf("total rewrites = %d, want 2", total)
	}
}

func TestMvE2E_PathQualifiedLinksRewritten(t *testing.T) {
	m := testkit.Wiki(
		testkit.F("wiki/locus/target.md", "---\n---\n# Target page\n"),
		testkit.F("wiki/refs.md", "---\n---\nSee [[locus/target]] and [[wiki/locus/target]].\n"),
	)

	eng := engine.New()
	result, err := mv.Move(m, "wiki/locus/target.md", "wiki/infra/target.md", eng, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := fs.ReadFile(m, "wiki/refs.md")
	body := string(data)

	// Path-qualified links rewritten even though stem unchanged
	if !strings.Contains(body, "[[infra/target]]") {
		t.Errorf("missing [[infra/target]], got: %s", body)
	}
	if !strings.Contains(body, "[[wiki/infra/target]]") {
		t.Errorf("missing [[wiki/infra/target]], got: %s", body)
	}
	if strings.Contains(body, "[[locus/target]]") {
		t.Errorf("old path-qualified link still present: %s", body)
	}

	total := 0
	for _, lu := range result.LinkUpdates {
		total += lu.Count
	}
	if total != 2 {
		t.Errorf("total rewrites = %d, want 2", total)
	}

	_ = result
}

func TestMvE2E_PathQualifiedAnchoredCombined(t *testing.T) {
	m := testkit.Wiki(
		testkit.F("wiki/locus/target.md", "---\n---\n# Target\n"),
		testkit.F("wiki/refs.md", "---\n---\n[[locus/target#heading|display text]]\n"),
	)

	eng := engine.New()
	_, err := mv.Move(m, "wiki/locus/target.md", "wiki/infra/renamed.md", eng, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := fs.ReadFile(m, "wiki/refs.md")
	body := string(data)
	want := "[[infra/renamed#heading|display text]]"
	if !strings.Contains(body, want) {
		t.Errorf("missing %q, got: %s", want, body)
	}
}

func TestMvE2E_FencedCodeUntouched(t *testing.T) {
	m := testkit.Wiki(
		testkit.F("wiki/locus/target.md", "---\n---\n# Target\n"),
		testkit.F("wiki/refs.md", "---\n---\n[[target#h]] outside\n```\n[[target#h]] inside code\n```\n[[locus/target]] after\n"),
	)

	eng := engine.New()
	_, err := mv.Move(m, "wiki/locus/target.md", "wiki/infra/renamed.md", eng, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := fs.ReadFile(m, "wiki/refs.md")
	body := string(data)

	// Outside fence: rewritten
	if !strings.Contains(body, "[[renamed#h]] outside") {
		t.Errorf("outside-fence link not rewritten: %s", body)
	}
	if !strings.Contains(body, "[[infra/renamed]] after") {
		t.Errorf("after-fence link not rewritten: %s", body)
	}
	// Inside fence: untouched
	if !strings.Contains(body, "[[target#h]] inside code") {
		t.Errorf("fenced link was modified: %s", body)
	}
}

func TestMvE2E_DirMovePathQualifiedRewritten(t *testing.T) {
	m := testkit.Wiki(
		testkit.F("wiki/sub/a.md", "---\n---\nContent A\n"),
		testkit.F("wiki/sub/b.md", "---\n---\nContent B\n"),
		testkit.F("wiki/index.md", "---\n---\nBare: [[a]]. Path: [[sub/a]]. Anchored: [[b#section]].\n"),
	)

	eng := engine.New()
	result, err := mv.Move(m, "wiki/sub", "wiki/moved", eng, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := fs.ReadFile(m, "wiki/index.md")
	body := string(data)

	// Bare stems unchanged (a→a, b→b) — no rewrite for bare links
	if !strings.Contains(body, "Bare: [[a]]") {
		t.Errorf("bare link should be unchanged: %s", body)
	}
	// Path-qualified: sub/a → moved/a
	if !strings.Contains(body, "Path: [[moved/a]]") {
		t.Errorf("path-qualified link not rewritten: %s", body)
	}
	// Anchored bare: b#section — stem unchanged, no rewrite needed
	if !strings.Contains(body, "Anchored: [[b#section]]") {
		t.Errorf("anchored bare link should be unchanged: %s", body)
	}

	// Only the path-qualified link should be rewritten
	total := 0
	for _, lu := range result.LinkUpdates {
		total += lu.Count
	}
	if total != 1 {
		t.Errorf("total rewrites = %d, want 1 (only path-qualified)", total)
	}
}

func TestMvE2E_DryRunPathQualified(t *testing.T) {
	m := testkit.Wiki(
		testkit.F("wiki/locus/target.md", "---\n---\n# Target\n"),
		testkit.F("wiki/refs.md", "---\n---\n[[locus/target#heading]] and [[target]].\n"),
	)

	origRefs, _ := fs.ReadFile(m, "wiki/refs.md")

	eng := engine.New()
	result, err := mv.Move(m, "wiki/locus/target.md", "wiki/infra/renamed.md", eng, nil, true)
	if err != nil {
		t.Fatal(err)
	}

	// Dry run: filesystem unchanged
	if _, err := fs.Stat(m, "wiki/locus/target.md"); err != nil {
		t.Error("dry run removed source")
	}
	nowRefs, _ := fs.ReadFile(m, "wiki/refs.md")
	if string(nowRefs) != string(origRefs) {
		t.Error("dry run modified refs.md")
	}

	// But link updates computed
	total := 0
	for _, lu := range result.LinkUpdates {
		total += lu.Count
	}
	if total != 2 {
		t.Errorf("dry-run link updates = %d, want 2", total)
	}
}
