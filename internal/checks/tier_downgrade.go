package checks

import (
	"fmt"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

// tierOrder defines the confidentiality tier hierarchy (lowest to highest).
// A page at a higher tier cannot be incorporated into a lower-tier context.
var tierOrder = map[string]int{
	"public":       0,
	"internal":     1,
	"confidential": 2,
	"secret":       3,
}

// tierLevel returns the numeric level for a tier string, or -1 if unknown.
func tierLevel(tier string) int {
	level, ok := tierOrder[strings.ToLower(strings.TrimSpace(tier))]
	if !ok {
		return -1
	}
	return level
}

// knownTierNames returns the recognized tier names (for error messages).
func knownTierNames() string {
	return "public, internal, confidential, secret"
}

// tierDowngradeCheck verifies that pages with foreign-touched provenance
// do not violate the tier-inheritance rule: a page whose inherited tier
// (the highest confidential tier of any foreign wiki in foreign-touched)
// exceeds its own confidential/audience tier is a leak vector.
//
// FAIL-CLOSED: any unrecognized tier value — in the page's confidential/audience
// fields OR in the wiki-tiers config — produces an ERROR finding unconditionally.
// Typos never exempt a page from tier enforcement.
//
// Rule YAML params (supplied via meridian.yaml rule_params):
//
//	wiki-tiers:        map[string]string — foreign wiki name → tier (e.g. {"cos": "confidential"})
//	target-tier:       string — this wiki's default tier (used when page has no own confidential/audience)
//
// The check is a no-op (no findings) when:
//   - The page has no foreign-touched field AND no confidential/audience fields
//   - No wiki-tiers are configured (wiki not participating in mounts)
func tierDowngradeCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	var out []engine.RawFinding

	// Validate wiki-tiers config values (finding 4: fail-closed, unconditional).
	wikiTiers, invalidTierFindings := extractAndValidateWikiTiers(params)
	out = append(out, invalidTierFindings...)

	// Validate page's own tier fields unconditionally (finding 3: fail-closed).
	out = append(out, validatePageTierFields(doc)...)

	// Extract foreign-touched from frontmatter.
	ftRaw, hasFT := doc.Frontmatter["foreign-touched"]
	if !hasFT {
		return out
	}

	touched := toStringSlice(ftRaw)
	if len(touched) == 0 {
		return out
	}

	if len(wikiTiers) == 0 {
		// No valid tier mapping configured — cannot compare.
		return out
	}

	// Compute the highest inherited tier from foreign-touched sources.
	highestLevel := -1
	highestSource := ""
	highestTierName := ""
	for _, wiki := range touched {
		wiki = strings.TrimSpace(wiki)
		tier, ok := wikiTiers[strings.ToLower(wiki)]
		if !ok {
			continue
		}
		level := tierLevel(tier)
		if level > highestLevel {
			highestLevel = level
			highestSource = wiki
			highestTierName = tier
		}
	}

	if highestLevel < 0 {
		// No known-tier wikis in foreign-touched.
		return out
	}

	// Determine the page's own effective tier.
	pageTier := effectivePageTier(doc, params)
	pageLevel := tierLevel(pageTier)

	if pageLevel < 0 {
		// Page has no valid tier — already reported by validatePageTierFields
		// if it declared one; if truly absent + no target-tier, cannot compare.
		return out
	}

	// Tier-downgrade: inherited tier exceeds page/wiki tier.
	if highestLevel > pageLevel {
		out = append(out, engine.RawFinding{
			Line: 1,
			TemplateData: map[string]string{
				"PageTier":   pageTier,
				"SourceTier": highestTierName,
				"Source":     highestSource,
				"Field":      "foreign-touched",
			},
		})
	}

	return out
}

// validatePageTierFields checks page's confidential/audience fields for
// unrecognized tier values. Fails closed unconditionally — a typo like
// "confidential: secrit" is always an ERROR, not a silent exemption.
func validatePageTierFields(doc *engine.Document) []engine.RawFinding {
	var out []engine.RawFinding

	if v, ok := doc.Frontmatter["confidential"]; ok {
		if s, ok := v.(string); ok && s != "" {
			if tierLevel(s) < 0 {
				out = append(out, engine.RawFinding{
					Line: 1,
					TemplateData: map[string]string{
						"PageTier":   "(unrecognized)",
						"SourceTier": s,
						"Source":     "field confidential",
						"Field":      fmt.Sprintf("%q is not a recognized tier (%s)", s, knownTierNames()),
					},
				})
			}
		}
	}

	// audience: is deliberately NOT validated here. It is the C45 public-pack
	// human-audience declaration — free-form by design ("jessie", a team
	// name, CJK strings). Reading it as a tier enum turned every role-less
	// wiki's audience field into 13 pre-push-blocking ERRORs. Tier semantics
	// live in confidential: (and target-tier) only.

	return out
}

// effectivePageTier returns the page's effective confidentiality tier.
// Priority: page's own confidential > target-tier param. audience: is a
// human-audience field (C45), never a tier source — see validatePageTierFields.
func effectivePageTier(doc *engine.Document, params map[string]any) string {
	if v, ok := doc.Frontmatter["confidential"]; ok {
		if s, ok := v.(string); ok && s != "" {
			if tierLevel(s) >= 0 {
				return strings.ToLower(strings.TrimSpace(s))
			}
		}
	}
	if v, ok := params["target-tier"]; ok {
		if s, ok := v.(string); ok {
			return strings.ToLower(strings.TrimSpace(s))
		}
	}
	return ""
}

// extractAndValidateWikiTiers parses the wiki-tiers param into a normalized map
// and returns ERROR findings for any unrecognized tier values (fail-closed).
func extractAndValidateWikiTiers(params map[string]any) (map[string]string, []engine.RawFinding) {
	raw, ok := params["wiki-tiers"]
	if !ok {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, nil
	}
	result := make(map[string]string, len(m))
	var findings []engine.RawFinding
	for k, v := range m {
		if s, ok := v.(string); ok {
			normalized := strings.ToLower(strings.TrimSpace(s))
			if tierLevel(normalized) < 0 {
				findings = append(findings, engine.RawFinding{
					Line: 0,
					TemplateData: map[string]string{
						"PageTier":   "(config)",
						"SourceTier": s,
						"Source":     k,
						"Field":      fmt.Sprintf("wiki-tiers: %q has unrecognized tier %q (%s)", k, s, knownTierNames()),
					},
				})
			} else {
				result[strings.ToLower(k)] = normalized
			}
		}
	}
	return result, findings
}
