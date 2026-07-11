// Package schema produces the effective wiki schema by merging the
// versioned contract schema with a wiki-local SCHEMA.md overlay.
// Merge rule: shallow, local wins (contract-v1 §11).
package schema

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/caoer/meridian/internal/frontmatter"
)

// contractSchemas maps contract version → baseline key/value pairs.
// Each value is the contract-defined default for that key.
//
// Drift guard: internal/contract/drift_test.go asserts the shared
// fields here match the embedded YAML rule pack. Edit either side
// and the test catches the divergence.
var contractSchemas = map[int]map[string]any{
	1: {
		"contract-version": 1,
		"layout": []map[string]any{
			{"dir": "SCHEMA.md", "purpose": "Contract version pin + local schema extensions", "required": true},
			{"dir": "index.md", "purpose": "Wiki entry point", "required": true},
			{"dir": "inbox", "purpose": "Raw drops (markdown only; binaries go to companion data repo)", "required": true},
			{"dir": "sources", "purpose": "Honest interpretations of ingested material", "required": true},
			{"dir": "domains", "purpose": "Evolving reasoning, organized by domain", "required": true},
			{"dir": "synthesis", "purpose": "Recipes and cross-cutting analyses", "required": true},
			{"dir": "outbox", "purpose": "Artifacts (markdown only; see outbox clause)", "required": true},
			{"dir": "logs", "purpose": "Operation log", "required": true},
			{"dir": "decisions", "purpose": "Lazy-decision queue", "required": true},
			{"dir": "foreign", "purpose": "Runtime mount of other wikis (gitignored)", "required": false},
		},
		"frontmatter_minima": []map[string]any{
			{"field": "tags", "type": "list of strings", "required": true},
			{"field": "created", "type": "date", "required": true},
		},
		"frontmatter_optional": []map[string]any{
			{"field": "audience", "type": "string", "when": "cross-wiki mount participant"},
			{"field": "confidential", "type": "string", "when": "cross-wiki mount participant"},
			{"field": "foreign-touched", "type": "list of strings", "when": "auto-stamped by skill"},
		},
		"addressing": map[string]any{
			"in_wiki":  "shortest-unambiguous canonical wikilinks",
			"cross":    "ingest-not-reference",
			"universe": "own-wiki files only; /foreign and data-repo excluded",
		},
		"decision_queue": map[string]any{
			"required_fields": []string{"type", "status", "created", "tags"},
			"valid_status":    []string{"pending", "approved", "rejected"},
			"confidence_vocab": []string{
				"certain", "high confidence", "moderate confidence",
				"low confidence", "speculative",
			},
		},
		"log_contract": map[string]any{
			"filename":        "YYYY-MM-DD-<op>-<slug>.md",
			"required_fields": []string{"type", "op", "created", "tags"},
		},
		"synthesis_contract": map[string]any{
			"required_type":  "synthesis",
			"required_field": "draws-from",
		},
		"check_spec": map[string]any{
			"severities": []string{"error", "warn", "note", "off"},
			"exit_codes": map[string]any{
				"0": "clean",
				"1": "error-severity findings",
				"2": "tool failure",
			},
		},
		"outbox": map[string]any{
			"default":   "markdown-only",
			"carve_out": "skills/<name>/ artifact dirs may carry constitutive scripts",
			"carve_out_paths": []string{
				"outbox/skills/*/scripts/**",
				"outbox/skills/*/agents/**",
			},
		},
		"leaf_repo": true,
	},
}

// Contract versions 2 and 3 are the v1 baseline with the ratified amendment
// deltas applied — each inherits every unchanged field by value from its
// predecessor, so the shared minima (frontmatter, decision queue, addressing,
// log/synthesis/check contracts, leaf_repo) stay byte-identical across versions.
// The drift guard (internal/contract/drift_test.go) asserts that invariant.
//
// Deltas trace to home-wiki SCHEMA.md amendment callouts + effects/EFFECTS.md;
// the human-readable law is the llm-wiki skill's references/contract-v3.md.
//
//   - v2 (2026-07-05, decision RESOLVED-domain-reorg): cluster layout as a MAY
//     (domains/[<cluster>/]<domain>/, clusters are pure containers, leaf names
//     globally unique) + UPPERCASE root convention files (index.md → INDEX.md).
//     The "catalogs are computed" clause is a home-wiki LOCAL overlay, not
//     contract law, so it is deliberately absent here (a v2 wiki MAY still
//     author an INDEX.md entry point).
//   - v3 (2026-07-06, decision effects-layer-inversion): the outbox content
//     tier inverts to the effects descriptor tier. The outbox clause + the
//     outbox-md-only rule retire; type/effect + a closed effect/<kind> set +
//     the four-part pin contract (point or own, never copy) land.
func init() {
	v1Layout := contractSchemas[1]["layout"].([]map[string]any)

	// v2 layout deltas.
	v2Layout := replaceLayoutDir(v1Layout, "index.md", map[string]any{
		"dir": "INDEX.md", "purpose": "Wiki entry point (UPPERCASE root convention file)", "required": true,
	})
	v2Layout = replaceLayoutDir(v2Layout, "domains", map[string]any{
		"dir":      "domains",
		"purpose":  "Evolving reasoning; domains/[<cluster>/]<domain>/ — cluster level is a MAY, clusters are pure containers, leaf domain names globally unique",
		"required": true,
	})
	v2 := copyMap(contractSchemas[1])
	v2["contract-version"] = 2
	v2["layout"] = v2Layout
	contractSchemas[2] = v2

	// v3 layout delta: outbox → effects (the descriptor tier).
	v3Layout := replaceLayoutDir(v2Layout, "outbox", map[string]any{
		"dir": "effects", "purpose": "Descriptor tier — one pin-verified page per effect (point or own)", "required": true,
	})
	v3 := copyMap(contractSchemas[2])
	v3["contract-version"] = 3
	v3["layout"] = v3Layout
	delete(v3, "outbox") // the outbox clause + outbox-md-only rule retire in v3
	v3["effects"] = map[string]any{
		"kinds":           []string{"skill", "agent", "prompt", "site", "document"},
		"page_tag":        "type/effect",
		"kind_tag_prefix": "effect/",
		"pin_contract":    []string{"repo", "commit", "location", "checksum", "branch"},
		"invariant":       "point or own, never copy",
	}
	contractSchemas[3] = v3
}

// replaceLayoutDir returns a copy of layout in which the entry whose "dir"
// equals oldDir is swapped for newEntry (order preserved). The source slice's
// maps are never mutated, so lower contract versions keep their layout intact.
func replaceLayoutDir(layout []map[string]any, oldDir string, newEntry map[string]any) []map[string]any {
	out := make([]map[string]any, len(layout))
	for i, e := range layout {
		if e["dir"] == oldDir {
			out[i] = newEntry
		} else {
			out[i] = e
		}
	}
	return out
}

// Effective produces the merged effective schema for a wiki rooted at root.
// It reads SCHEMA.md from root, extracts the contract-version pin, loads
// the contract baseline for that version, then shallow-merges with local
// wins. Missing SCHEMA.md returns the contract-only schema for version 1.
// Malformed SCHEMA.md returns an error naming the file and problem.
func Effective(root string) (map[string]any, error) {
	schemaPath := filepath.Join(root, "SCHEMA.md")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing local SCHEMA.md → contract-only, not an error.
			return copyMap(contractSchemas[1]), nil
		}
		return nil, fmt.Errorf("%s: %w", schemaPath, err)
	}

	doc, err := frontmatter.ParseBytes(data)
	if err != nil {
		// Include file:line when the parser provides a line number.
		if pe, ok := err.(*frontmatter.ParseError); ok {
			return nil, fmt.Errorf("%s:%d: %w", schemaPath, pe.Line, pe)
		}
		return nil, fmt.Errorf("%s: %w", schemaPath, err)
	}
	if doc == nil {
		// No frontmatter at all — contract-only.
		return copyMap(contractSchemas[1]), nil
	}

	// Determine contract version from local overlay.
	version := 1
	if cv, ok := doc.Meta["contract-version"]; ok {
		switch v := cv.(type) {
		case int:
			version = v
		case float64:
			version = int(v)
		default:
			return nil, fmt.Errorf("%s: contract-version must be an integer, got %T", schemaPath, cv)
		}
	}

	contract, ok := contractSchemas[version]
	if !ok {
		return nil, fmt.Errorf("%s: unknown contract-version %d", schemaPath, version)
	}

	// Shallow merge: start with contract, local wins.
	merged := copyMap(contract)
	for k, v := range doc.Meta {
		merged[k] = v
	}

	return merged, nil
}

// ContractV1 returns a shallow copy of the contract-version-1 schema map.
// Exported for the drift guard test in internal/contract.
func ContractV1() map[string]any {
	return copyMap(contractSchemas[1])
}

// Contract returns a shallow copy of the contract schema for version v and
// whether that version exists. Exported for the drift guard, which asserts the
// shared fields agree across every materialized version.
func Contract(v int) (map[string]any, bool) {
	c, ok := contractSchemas[v]
	if !ok {
		return nil, false
	}
	return copyMap(c), true
}

// copyMap returns a shallow copy of a map[string]any.
func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
