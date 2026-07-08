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
	"wikilink-canonicalize":      wikilinkCanonicalizeCheck,
	"tier-downgrade":             tierDowngradeCheck,
}
