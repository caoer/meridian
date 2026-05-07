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
		if lu.OldLink != "design-doc" {
			t.Errorf("old_link = %q, want design-doc", lu.OldLink)
		}
		if lu.NewLink != "arch-doc" {
			t.Errorf("new_link = %q, want arch-doc", lu.NewLink)
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
