package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// --- tierLevel ---

func TestTierLevel_KnownTiers(t *testing.T) {
	cases := []struct {
		tier  string
		level int
	}{
		{"public", 0},
		{"internal", 1},
		{"confidential", 2},
		{"secret", 3},
	}
	for _, c := range cases {
		if got := tierLevel(c.tier); got != c.level {
			t.Errorf("tierLevel(%q) = %d, want %d", c.tier, got, c.level)
		}
	}
}

func TestTierLevel_CaseInsensitive(t *testing.T) {
	if tierLevel("Public") != 0 {
		t.Error("want Public → 0")
	}
	if tierLevel("CONFIDENTIAL") != 2 {
		t.Error("want CONFIDENTIAL → 2")
	}
}

func TestTierLevel_Unknown(t *testing.T) {
	if tierLevel("unknown") != -1 {
		t.Error("want -1 for unknown tier")
	}
	if tierLevel("") != -1 {
		t.Error("want -1 for empty string")
	}
}

// ==========================================================================
// REFUSAL TESTS: tier-downgrade must produce findings
// ==========================================================================

func TestTierDowngrade_ConfidentialIntoPublicPage(t *testing.T) {
	doc := &engine.Document{
		Path: "synthesis/summary.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"cos-wiki"},
			"confidential":    "public",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"cos-wiki": "confidential",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	found := findByField(findings, "foreign-touched")
	if found == nil {
		t.Fatalf("REFUSAL PROOF: want tier-downgrade finding, got none among %d findings", len(findings))
	}
	if found.TemplateData["PageTier"] != "public" {
		t.Errorf("PageTier = %q, want public", found.TemplateData["PageTier"])
	}
	if found.TemplateData["SourceTier"] != "confidential" {
		t.Errorf("SourceTier = %q, want confidential", found.TemplateData["SourceTier"])
	}
	if found.TemplateData["Source"] != "cos-wiki" {
		t.Errorf("Source = %q, want cos-wiki", found.TemplateData["Source"])
	}
}

func TestTierDowngrade_SecretIntoInternalPage(t *testing.T) {
	doc := &engine.Document{
		Path: "domains/analysis.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"secret-wiki"},
			"confidential":    "internal",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"secret-wiki": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	found := findByField(findings, "foreign-touched")
	if found == nil {
		t.Fatalf("REFUSAL PROOF: want finding for secret→internal downgrade, got none among %d", len(findings))
	}
	if found.TemplateData["SourceTier"] != "secret" {
		t.Errorf("SourceTier = %q, want secret", found.TemplateData["SourceTier"])
	}
}

func TestTierDowngrade_HighestTierWins(t *testing.T) {
	doc := &engine.Document{
		Path: "sources/multi.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"public-wiki", "secret-wiki"},
			"confidential":    "internal",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"public-wiki": "public",
			"secret-wiki": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	found := findByField(findings, "foreign-touched")
	if found == nil {
		t.Fatalf("REFUSAL PROOF: want finding (highest source=secret > page=internal), got none among %d", len(findings))
	}
	if found.TemplateData["Source"] != "secret-wiki" {
		t.Errorf("Source = %q, want secret-wiki (highest tier source)", found.TemplateData["Source"])
	}
}

func TestTierDowngrade_UsesTargetTierWhenPageHasNoOwnTier(t *testing.T) {
	doc := &engine.Document{
		Path: "sources/bare.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"secret-wiki"},
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"secret-wiki": "secret",
		},
		"target-tier": "public",
	}
	findings := tierDowngradeCheck(doc, params)
	found := findByField(findings, "foreign-touched")
	if found == nil {
		t.Fatalf("REFUSAL PROOF: want finding (target-tier=public, source=secret), got none among %d", len(findings))
	}
	if found.TemplateData["PageTier"] != "public" {
		t.Errorf("PageTier = %q, want public (from target-tier)", found.TemplateData["PageTier"])
	}
}

func TestTierDowngrade_ConfidentialFieldTakesPriorityOverAudience(t *testing.T) {
	// audience: is a human-audience field (C45) — even a value that happens
	// to spell a tier name must not affect the comparison.
	doc := &engine.Document{
		Path: "sources/both-fields.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"cos"},
			"confidential":    "public",
			"audience":        "secret",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"cos": "confidential",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	found := findByField(findings, "foreign-touched")
	if found == nil {
		t.Fatalf("REFUSAL PROOF: confidential=public should be used (not audience=secret), got none among %d", len(findings))
	}
	if found.TemplateData["PageTier"] != "public" {
		t.Errorf("PageTier = %q, want public (confidential takes priority)", found.TemplateData["PageTier"])
	}
}

func TestTierDowngrade_ScalarStringForeignTouched(t *testing.T) {
	// Finding 2: scalar string `foreign-touched: cos` (no brackets) must not evade.
	doc := &engine.Document{
		Path: "sources/scalar.md",
		Frontmatter: map[string]any{
			"foreign-touched": "secret-wiki", // scalar, not list
			"confidential":    "public",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"secret-wiki": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	found := findByField(findings, "foreign-touched")
	if found == nil {
		t.Fatalf("REFUSAL PROOF (fail-closed): scalar foreign-touched must not evade, got none among %d", len(findings))
	}
}

// ==========================================================================
// FAIL-CLOSED REFUSAL: unrecognized tiers produce ERROR, never exemption
// ==========================================================================

func TestTierDowngrade_FailClosed_PageTypoConfidential(t *testing.T) {
	// Finding 3: 'confidential: secrit' typo → ERROR unconditionally.
	doc := &engine.Document{
		Path: "sources/typo.md",
		Frontmatter: map[string]any{
			"confidential": "secrit", // typo
		},
	}
	params := map[string]any{}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("REFUSAL PROOF (fail-closed): typo tier must produce finding, got %d", len(findings))
	}
	f := findings[0]
	if f.TemplateData["Source"] != "field confidential" {
		t.Errorf("Source = %q, want 'field confidential'", f.TemplateData["Source"])
	}
	if f.TemplateData["PageTier"] != "(unrecognized)" {
		t.Errorf("PageTier = %q, want '(unrecognized)'", f.TemplateData["PageTier"])
	}
	if f.TemplateData["SourceTier"] != "secrit" {
		t.Errorf("SourceTier = %q, want 'secrit' (the invalid value)", f.TemplateData["SourceTier"])
	}
}

func TestTierDowngrade_AudienceIsFreeForm_NeverAFinding(t *testing.T) {
	// The home-wiki misfire class: audience: carries human names, team
	// strings, CJK — it is the C45 audience declaration, not a tier enum.
	for _, aud := range []string{"jessie", "coscene-engineering-team", "ZT (morning review)", "所有工程师", "publik"} {
		doc := &engine.Document{
			Path:        "sources/human-audience.md",
			Frontmatter: map[string]any{"audience": aud},
		}
		if findings := tierDowngradeCheck(doc, map[string]any{}); len(findings) != 0 {
			t.Errorf("audience=%q must never produce a tier finding, got %d", aud, len(findings))
		}
	}
}

func TestTierDowngrade_FailClosed_BothFieldsTypo(t *testing.T) {
	doc := &engine.Document{
		Path: "sources/both-typo.md",
		Frontmatter: map[string]any{
			"confidential": "secrit",
			"audience":     "publik",
		},
	}
	params := map[string]any{}
	findings := tierDowngradeCheck(doc, params)
	// Only confidential: is a tier field — the audience typo is free-form text.
	if len(findings) != 1 {
		t.Fatalf("REFUSAL PROOF (fail-closed): confidential typo must produce exactly 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Source"] != "field confidential" {
		t.Errorf("Source = %q, want 'field confidential'", findings[0].TemplateData["Source"])
	}
}

func TestTierDowngrade_FailClosed_WikiTiersInvalidValue(t *testing.T) {
	// Finding 4: invalid wiki-tiers value → ERROR unconditionally.
	doc := &engine.Document{
		Path:        "sources/any.md",
		Frontmatter: map[string]any{},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"bad-wiki": "ultra-secret", // not a recognized tier
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("REFUSAL PROOF (fail-closed): invalid wiki-tiers value must produce finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Source"] != "bad-wiki" {
		t.Errorf("Source = %q, want bad-wiki", findings[0].TemplateData["Source"])
	}
}

func TestTierDowngrade_FailClosed_WikiTiersMultipleInvalid(t *testing.T) {
	doc := &engine.Document{
		Path:        "sources/any.md",
		Frontmatter: map[string]any{},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"good-wiki": "secret",
			"bad-wiki1": "ultra-secret",
			"bad-wiki2": "classified",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings for 2 invalid wiki-tier values, got %d", len(findings))
	}
}

func TestTierDowngrade_FailClosed_TypoDoesNotExemptFromDowngrade(t *testing.T) {
	// Page has typo confidential + foreign-touched. The typo gets its own
	// finding; the downgrade comparison falls back to target-tier (audience
	// is never a tier source).
	doc := &engine.Document{
		Path: "sources/typo-plus-downgrade.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"secret-wiki"},
			"confidential":    "secrit", // typo — own finding
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"secret-wiki": "secret",
		},
		"target-tier": "public",
	}
	findings := tierDowngradeCheck(doc, params)
	// Expect: 1 typo finding + 1 downgrade finding = 2
	if len(findings) != 2 {
		t.Fatalf("want 2 findings (typo + downgrade), got %d", len(findings))
	}
	var hasTypo, hasDowngrade bool
	for _, f := range findings {
		if f.TemplateData["Source"] == "field confidential" {
			hasTypo = true
		}
		if f.TemplateData["Field"] == "foreign-touched" {
			hasDowngrade = true
		}
	}
	if !hasTypo {
		t.Error("REFUSAL PROOF: missing typo finding for confidential")
	}
	if !hasDowngrade {
		t.Error("REFUSAL PROOF: missing downgrade finding")
	}
}

// ==========================================================================
// CLEAN PASS TESTS: no findings when tiers are compatible
// ==========================================================================

func TestTierDowngrade_SameTier_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Path: "sources/peer.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"team-wiki"},
			"confidential":    "internal",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"team-wiki": "internal",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for same-tier (internal=internal), got %d", len(findings))
	}
}

func TestTierDowngrade_UpgradeTier_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Path: "sources/upgrade.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"public-wiki"},
			"confidential":    "secret",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"public-wiki": "public",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for tier upgrade (public→secret), got %d", len(findings))
	}
}

func TestTierDowngrade_ValidTierFields_NoFinding(t *testing.T) {
	// Page has valid tier fields, no foreign-touched — clean.
	doc := &engine.Document{
		Path: "sources/clean.md",
		Frontmatter: map[string]any{
			"confidential": "internal",
			"audience":     "public",
		},
	}
	params := map[string]any{}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for valid tier fields without foreign-touched, got %d", len(findings))
	}
}

// ==========================================================================
// EDGE CASES
// ==========================================================================

func TestTierDowngrade_NoForeignTouched_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Path:        "domains/local.md",
		Frontmatter: map[string]any{},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"cos": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings when no foreign-touched and no tier fields, got %d", len(findings))
	}
}

func TestTierDowngrade_EmptyForeignTouched_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Path: "domains/empty.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{},
			"confidential":    "public",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"cos": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty foreign-touched with valid tier, got %d", len(findings))
	}
}

func TestTierDowngrade_NoWikiTiers_NoDowngradeFinding(t *testing.T) {
	doc := &engine.Document{
		Path: "sources/orphan.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"some-wiki"},
			"confidential":    "public",
		},
	}
	params := map[string]any{}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings when no wiki-tiers configured and valid page tier, got %d", len(findings))
	}
}

func TestTierDowngrade_ForeignTouchedWikiNotInTierMap_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Path: "sources/unknown-source.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"unknown-wiki"},
			"confidential":    "public",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"cos": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings when foreign-touched wiki not in tier map, got %d", len(findings))
	}
}

func TestTierDowngrade_NilFrontmatter_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Path:        "sources/nil-fm.md",
		Frontmatter: nil,
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"cos": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for nil frontmatter, got %d", len(findings))
	}
}

func TestTierDowngrade_ForeignTouchedAsStringSlice(t *testing.T) {
	doc := &engine.Document{
		Path: "sources/string-slice.md",
		Frontmatter: map[string]any{
			"foreign-touched": []string{"secret-wiki"},
			"confidential":    "public",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"secret-wiki": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	found := findByField(findings, "foreign-touched")
	if found == nil {
		t.Fatalf("want downgrade finding for []string foreign-touched, got none among %d", len(findings))
	}
}

func TestTierDowngrade_AudienceNeverATierSource(t *testing.T) {
	// audience: is free-form (C45) — even "public" there is a human word,
	// not a tier. With no confidential: and no target-tier, the page has no
	// tier to compare against: no downgrade finding.
	doc := &engine.Document{
		Path: "sources/audience-only.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"secret-wiki"},
			"audience":        "public",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"secret-wiki": "secret",
		},
	}
	if findings := tierDowngradeCheck(doc, params); len(findings) != 0 {
		t.Fatalf("audience must not feed the tier comparison, got %d findings", len(findings))
	}
}

func TestTierDowngrade_FindingLine(t *testing.T) {
	doc := &engine.Document{
		Path: "sources/line-check.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"cos"},
			"confidential":    "public",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"cos": "confidential",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	found := findByField(findings, "foreign-touched")
	if found == nil {
		t.Fatalf("want finding, got none among %d", len(findings))
	}
	if found.Line != 1 {
		t.Errorf("Line = %d, want 1 (frontmatter-level finding)", found.Line)
	}
}

func TestTierDowngrade_MissingTierFieldsNoForeignTouched_NoFinding(t *testing.T) {
	// Page with no tier fields and no foreign-touched — truly uninvolved.
	doc := &engine.Document{
		Path: "sources/plain.md",
		Frontmatter: map[string]any{
			"tags":    []any{"type/source"},
			"created": "2026-01-01",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"cos": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for page with no tier fields and no foreign-touched, got %d", len(findings))
	}
}

// --- extractAndValidateWikiTiers ---

func TestExtractWikiTiers_Valid(t *testing.T) {
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"COS":  "Confidential",
			"home": "secret",
		},
	}
	got, findings := extractAndValidateWikiTiers(params)
	if len(findings) != 0 {
		t.Errorf("want 0 validation findings for valid tiers, got %d", len(findings))
	}
	if got["cos"] != "confidential" {
		t.Errorf("cos = %q, want confidential", got["cos"])
	}
	if got["home"] != "secret" {
		t.Errorf("home = %q, want secret", got["home"])
	}
}

func TestExtractWikiTiers_Missing(t *testing.T) {
	got, findings := extractAndValidateWikiTiers(map[string]any{})
	if got != nil {
		t.Errorf("want nil for missing wiki-tiers, got %v", got)
	}
	if len(findings) != 0 {
		t.Errorf("want 0 findings, got %d", len(findings))
	}
}

func TestExtractWikiTiers_WrongType(t *testing.T) {
	got, findings := extractAndValidateWikiTiers(map[string]any{"wiki-tiers": "not a map"})
	if got != nil {
		t.Errorf("want nil for wrong type, got %v", got)
	}
	if len(findings) != 0 {
		t.Errorf("want 0 findings, got %d", len(findings))
	}
}

func TestExtractWikiTiers_InvalidValue(t *testing.T) {
	got, findings := extractAndValidateWikiTiers(map[string]any{
		"wiki-tiers": map[string]any{
			"good": "secret",
			"bad":  "ultra-secret",
		},
	})
	if got["good"] != "secret" {
		t.Errorf("good = %q, want secret", got["good"])
	}
	if _, exists := got["bad"]; exists {
		t.Error("bad should not be in valid tiers map")
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 validation finding for invalid tier, got %d", len(findings))
	}
}

// --- effectivePageTier ---

func TestEffectivePageTier_ConfidentialFirst(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"confidential": "secret",
			"audience":     "public",
		},
	}
	got := effectivePageTier(doc, map[string]any{})
	if got != "secret" {
		t.Errorf("got %q, want secret (confidential takes priority)", got)
	}
}

func TestEffectivePageTier_AudienceIgnored(t *testing.T) {
	// audience: is free-form (C45), never a tier source.
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"audience": "internal",
		},
	}
	if got := effectivePageTier(doc, map[string]any{}); got != "" {
		t.Errorf("got %q, want empty (audience is not a tier source)", got)
	}
}

func TestEffectivePageTier_TargetTierFallback(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{},
	}
	got := effectivePageTier(doc, map[string]any{"target-tier": "public"})
	if got != "public" {
		t.Errorf("got %q, want public (target-tier fallback)", got)
	}
}

func TestEffectivePageTier_NoTier(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{},
	}
	got := effectivePageTier(doc, map[string]any{})
	if got != "" {
		t.Errorf("got %q, want empty string (no tier available)", got)
	}
}

func TestEffectivePageTier_TypoSkipped(t *testing.T) {
	// Typo in confidential → skipped (separately reported), falls to target-tier.
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"confidential": "secrit",
		},
	}
	got := effectivePageTier(doc, map[string]any{"target-tier": "public"})
	if got != "public" {
		t.Errorf("got %q, want public (typo confidential skipped, target-tier fallback)", got)
	}
}

// ==========================================================================
// MESSAGE TEMPLATE WIRING: verify TemplateData renders coherently
// ==========================================================================

func TestTierDowngrade_MessageWiring_UnrecognizedPageTier(t *testing.T) {
	// Regression: previously the unrecognized-tier finding put the field name
	// ("confidential") in Source and the invalid value ("secrit") in PageTier,
	// producing "page at secrit inherits unrecognized from foreign-touched wiki confidential".
	doc := &engine.Document{
		Path: "sources/typo.md",
		Frontmatter: map[string]any{
			"confidential": "secrit",
		},
	}
	findings := tierDowngradeCheck(doc, map[string]any{})
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	f := findings[0]
	// SourceTier carries the invalid value so the message reads
	// "inherits secrit" (what was declared) rather than "inherits unrecognized".
	if f.TemplateData["SourceTier"] != "secrit" {
		t.Errorf("SourceTier = %q, want secrit", f.TemplateData["SourceTier"])
	}
	// Source names the field (prefixed) so the message reads
	// "from foreign-touched wiki field confidential" — not "wiki confidential".
	if f.TemplateData["Source"] != "field confidential" {
		t.Errorf("Source = %q, want 'field confidential'", f.TemplateData["Source"])
	}
	// PageTier signals this is a validation error, not a real tier level.
	if f.TemplateData["PageTier"] != "(unrecognized)" {
		t.Errorf("PageTier = %q, want '(unrecognized)'", f.TemplateData["PageTier"])
	}
}

// --- helpers ---

// findByField returns the first finding whose Field matches, or nil.
func findByField(findings []engine.RawFinding, field string) *engine.RawFinding {
	for i, f := range findings {
		if f.TemplateData["Field"] == field {
			return &findings[i]
		}
	}
	return nil
}
