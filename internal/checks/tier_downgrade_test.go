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

// --- REFUSAL TESTS: tier-downgrade must produce findings ---

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
	if len(findings) != 1 {
		t.Fatalf("REFUSAL PROOF: want 1 finding for tier downgrade, got %d", len(findings))
	}
	f := findings[0]
	if f.TemplateData["PageTier"] != "public" {
		t.Errorf("PageTier = %q, want public", f.TemplateData["PageTier"])
	}
	if f.TemplateData["SourceTier"] != "confidential" {
		t.Errorf("SourceTier = %q, want confidential", f.TemplateData["SourceTier"])
	}
	if f.TemplateData["Source"] != "cos-wiki" {
		t.Errorf("Source = %q, want cos-wiki", f.TemplateData["Source"])
	}
	if f.TemplateData["Field"] != "foreign-touched" {
		t.Errorf("Field = %q, want foreign-touched", f.TemplateData["Field"])
	}
}

func TestTierDowngrade_SecretIntoInternalPage(t *testing.T) {
	doc := &engine.Document{
		Path: "domains/analysis.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"secret-wiki"},
			"audience":        "internal",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"secret-wiki": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("REFUSAL PROOF: want 1 finding for secret→internal downgrade, got %d", len(findings))
	}
	if findings[0].TemplateData["SourceTier"] != "secret" {
		t.Errorf("SourceTier = %q, want secret", findings[0].TemplateData["SourceTier"])
	}
}

func TestTierDowngrade_HighestTierWins(t *testing.T) {
	// Page touches two wikis: one public, one secret. Highest (secret) wins.
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
	if len(findings) != 1 {
		t.Fatalf("REFUSAL PROOF: want 1 finding (highest source tier=secret > page=internal), got %d", len(findings))
	}
	if findings[0].TemplateData["Source"] != "secret-wiki" {
		t.Errorf("Source = %q, want secret-wiki (highest tier source)", findings[0].TemplateData["Source"])
	}
}

func TestTierDowngrade_UsesTargetTierWhenPageHasNoOwnTier(t *testing.T) {
	// Page has no confidential/audience field; falls back to target-tier.
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
	if len(findings) != 1 {
		t.Fatalf("REFUSAL PROOF: want 1 finding (page target-tier=public, source=secret), got %d", len(findings))
	}
	if findings[0].TemplateData["PageTier"] != "public" {
		t.Errorf("PageTier = %q, want public (from target-tier)", findings[0].TemplateData["PageTier"])
	}
}

func TestTierDowngrade_ConfidentialFieldTakesPriorityOverAudience(t *testing.T) {
	// Page has both confidential and audience; confidential wins.
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
	if len(findings) != 1 {
		t.Fatalf("REFUSAL PROOF: confidential=public should be used (not audience=secret), got %d findings", len(findings))
	}
	if findings[0].TemplateData["PageTier"] != "public" {
		t.Errorf("PageTier = %q, want public (confidential field takes priority)", findings[0].TemplateData["PageTier"])
	}
}

// --- CLEAN PASS TESTS: no findings when tiers are compatible ---

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
	// Page at secret, source at public — not a downgrade.
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

// --- EDGE CASES ---

func TestTierDowngrade_NoForeignTouched_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Path:        "domains/local.md",
		Frontmatter: map[string]any{"confidential": "public"},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"cos": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings when no foreign-touched, got %d", len(findings))
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
		t.Fatalf("want 0 findings for empty foreign-touched, got %d", len(findings))
	}
}

func TestTierDowngrade_NoWikiTiers_NoFinding(t *testing.T) {
	// Wiki not participating in mounts — no wiki-tiers configured.
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
		t.Fatalf("want 0 findings when no wiki-tiers configured, got %d", len(findings))
	}
}

func TestTierDowngrade_ForeignTouchedWikiNotInTierMap_NoFinding(t *testing.T) {
	// foreign-touched lists a wiki that's not in wiki-tiers.
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

func TestTierDowngrade_MissingFieldsOnNonMountWiki_NoFinding(t *testing.T) {
	// Page has foreign-touched but no confidential/audience AND no target-tier.
	// Fields are optional unless mount participant — no finding.
	doc := &engine.Document{
		Path: "sources/bare-no-target.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"secret-wiki"},
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"secret-wiki": "secret",
		},
		// No target-tier — wiki not participating as a target.
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings when page has no tier and no target-tier, got %d", len(findings))
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
	// YAML sometimes deserializes as []string instead of []any.
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
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for []string foreign-touched, got %d", len(findings))
	}
}

func TestTierDowngrade_AudienceFallback(t *testing.T) {
	// Page has audience but not confidential.
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
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding using audience fallback, got %d", len(findings))
	}
	if findings[0].TemplateData["PageTier"] != "public" {
		t.Errorf("PageTier = %q, want public (from audience)", findings[0].TemplateData["PageTier"])
	}
}

func TestTierDowngrade_UnrecognizedPageTier_NoFinding(t *testing.T) {
	// Page has a tier value not in the hierarchy.
	doc := &engine.Document{
		Path: "sources/custom-tier.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"cos"},
			"confidential":    "custom-level",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"cos": "secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for unrecognized page tier, got %d", len(findings))
	}
}

func TestTierDowngrade_UnrecognizedSourceTier_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Path: "sources/custom-source.md",
		Frontmatter: map[string]any{
			"foreign-touched": []any{"weird-wiki"},
			"confidential":    "public",
		},
	}
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"weird-wiki": "ultra-secret",
		},
	}
	findings := tierDowngradeCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for unrecognized source tier, got %d", len(findings))
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
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].Line != 1 {
		t.Errorf("Line = %d, want 1 (frontmatter-level finding)", findings[0].Line)
	}
}

// --- extractWikiTiers ---

func TestExtractWikiTiers_Valid(t *testing.T) {
	params := map[string]any{
		"wiki-tiers": map[string]any{
			"COS":  "Confidential",
			"home": "secret",
		},
	}
	got := extractWikiTiers(params)
	if got["cos"] != "confidential" {
		t.Errorf("cos = %q, want confidential", got["cos"])
	}
	if got["home"] != "secret" {
		t.Errorf("home = %q, want secret", got["home"])
	}
}

func TestExtractWikiTiers_Missing(t *testing.T) {
	got := extractWikiTiers(map[string]any{})
	if got != nil {
		t.Errorf("want nil for missing wiki-tiers, got %v", got)
	}
}

func TestExtractWikiTiers_WrongType(t *testing.T) {
	got := extractWikiTiers(map[string]any{"wiki-tiers": "not a map"})
	if got != nil {
		t.Errorf("want nil for wrong type, got %v", got)
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

func TestEffectivePageTier_AudienceFallback(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"audience": "internal",
		},
	}
	got := effectivePageTier(doc, map[string]any{})
	if got != "internal" {
		t.Errorf("got %q, want internal (audience fallback)", got)
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
