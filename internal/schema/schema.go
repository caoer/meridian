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

// copyMap returns a shallow copy of a map[string]any.
func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
