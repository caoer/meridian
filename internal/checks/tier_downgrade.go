package checks

import (
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

// tierDowngradeCheck verifies that pages with foreign-touched provenance
// do not violate the tier-inheritance rule: a page whose inherited tier
// (the highest confidential tier of any foreign wiki in foreign-touched)
// exceeds its own confidential/audience tier is a leak vector.
//
// Rule YAML params:
//
//	wiki-tiers:        map[string]string — foreign wiki name → tier (e.g. {"cos": "confidential"})
//	target-tier:       string — this wiki's default tier (used when page has no own confidential/audience)
//
// The check is a no-op (no findings) when:
//   - The page has no foreign-touched field
//   - No wiki-tiers are configured (wiki not participating in mounts)
//   - foreign-touched lists wikis not present in wiki-tiers
func tierDowngradeCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	// Extract foreign-touched from frontmatter.
	ftRaw, ok := doc.Frontmatter["foreign-touched"]
	if !ok {
		return nil
	}

	touched := toStringSlice(ftRaw)
	if len(touched) == 0 {
		return nil
	}

	// Extract wiki-tiers param.
	wikiTiers := extractWikiTiers(params)
	if len(wikiTiers) == 0 {
		// No tier mapping configured — wiki not participating in mounts,
		// fields are optional per contract. No finding.
		return nil
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
			highestTierName = strings.ToLower(tier)
		}
	}

	if highestLevel < 0 {
		// No known-tier wikis in foreign-touched.
		return nil
	}

	// Determine the page's own effective tier.
	pageTier := effectivePageTier(doc, params)
	pageLevel := tierLevel(pageTier)

	if pageLevel < 0 {
		// Page has no recognizable tier and no target-tier configured.
		// Cannot compare — no finding (fields optional unless mount participant).
		return nil
	}

	// Tier-downgrade: inherited tier exceeds page/wiki tier.
	if highestLevel > pageLevel {
		return []engine.RawFinding{{
			Line: 1,
			TemplateData: map[string]string{
				"PageTier":   pageTier,
				"SourceTier": highestTierName,
				"Source":     highestSource,
				"Field":      "foreign-touched",
			},
		}}
	}

	return nil
}

// effectivePageTier returns the page's effective confidentiality tier.
// Priority: page's own confidential > page's own audience > target-tier param.
func effectivePageTier(doc *engine.Document, params map[string]any) string {
	// Check page's own confidential field first.
	if v, ok := doc.Frontmatter["confidential"]; ok {
		if s, ok := v.(string); ok && tierLevel(s) >= 0 {
			return strings.ToLower(strings.TrimSpace(s))
		}
	}
	// Fall back to audience.
	if v, ok := doc.Frontmatter["audience"]; ok {
		if s, ok := v.(string); ok && tierLevel(s) >= 0 {
			return strings.ToLower(strings.TrimSpace(s))
		}
	}
	// Fall back to target-tier param (the wiki's default tier).
	if v, ok := params["target-tier"]; ok {
		if s, ok := v.(string); ok {
			return strings.ToLower(strings.TrimSpace(s))
		}
	}
	return ""
}

// extractWikiTiers parses the wiki-tiers param into a normalized map.
func extractWikiTiers(params map[string]any) map[string]string {
	raw, ok := params["wiki-tiers"]
	if !ok {
		return nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			result[strings.ToLower(k)] = strings.ToLower(strings.TrimSpace(s))
		}
	}
	return result
}
