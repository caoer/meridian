// Package checks provides built-in check implementations for meridian.
// Stage 1 workers: add new checks as separate files in this package,
// then register them in the All map.
package checks

import (
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/partition"
)

// All is the registry of built-in checks. Each entry maps a check name
// (as referenced by rule YAML "check" field) to its implementation.
var All = map[string]engine.CheckFunc{
	"field-exists":               fieldExistsCheck,
	"tag-format":                 tagFormatCheck,
	"pattern":                    patternCheck,
	"backticked-wikilink":        backtickWikilinkCheck,
	"link-resolve":               linkResolveCheck,
	"heading-structure":          headingStructureCheck,
	"partition-date-consistency": partition.CheckDateConsistency,
	"partition-rollup":           partition.CheckRollup,
	"property":                   propertyCheck,
	"broken-wikilink":            brokenWikilinkCheck,
	"ambiguous-wikilink":         ambiguousWikilinkCheck,
	"table-wikilink-pipe":        tableWikilinkPipeCheck,
	"effect-pin-resolves":        effectPinResolvesCheck,
	"effect-pin-on-origin":       effectPinOnOriginCheck,
	"effect-checksum-reproduces": effectChecksumReproducesCheck,
	"effect-pin-stale":           effectPinStaleCheck,
	"effect-unpinned":            effectUnpinnedCheck,
	"probe":                      probeCheck,
	"stale-run-record":           staleRunRecordCheck,
	"wikilink-canonicalize":      wikilinkCanonicalizeCheck,
	"tier-downgrade":             tierDowngradeCheck,
	"wiki-navlink":               wikiNavlinkCheck,
}

// Phase2 lists the checks whose verdict depends on state outside a single
// document's own bytes — the corpus path universe (link resolution against
// __scanned_paths) or, once U6 lands, external git state. The engine runs these
// in a separate pass over per-run snapshots and NEVER persists their findings to
// the per-doc cache: a cached verdict would go stale when an unrelated file is
// added or removed (the Ruff INP001 bug class). Register via engine.MarkPhase2.
//
// The link family here consumes engine facts (__facts) + __scanned_paths.
// Doc-local link checks are deliberately absent: table-wikilink-pipe (detects
// unescaped pipes in a row — no cross-file input) and wiki-navlink / backticked-
// wikilink (pure body grammar) stay phase-1 and remain cacheable. U6 appends the
// effect-pin family (effect-pin-resolves, effect-pin-on-origin,
// effect-checksum-reproduces, effect-pin-stale, effect-unpinned).
var Phase2 = []string{
	"broken-wikilink",
	"ambiguous-wikilink",
	"wikilink-canonicalize",
	"link-resolve",
	"property",
}
