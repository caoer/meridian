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
	"effect-rootless":            effectRootlessCheck,
	"body-value-echo":            bodyValueEchoCheck,
	"repo-cataloged":             repoCatalogedCheck,
	"chain-fresh":                chainFreshCheck,
	"probe":                      probeCheck,
	"stale-run-record":           staleRunRecordCheck,
	"wikilink-canonicalize":      wikilinkCanonicalizeCheck,
	"tier-downgrade":             tierDowngradeCheck,
	"wiki-navlink":               wikiNavlinkCheck,
}

// Phase2 lists the checks whose verdict depends on state outside a single
// document's own bytes — the corpus path universe (link resolution against
// __scanned_paths), external program output (probe), or external git state
// (the effect-pin family, added by U6). The engine runs these in a separate
// pass over per-run snapshots and NEVER persists their findings to the per-doc
// cache: a cached verdict would go stale when the external state changes with
// the doc unchanged (the Ruff INP001 bug class). Register via engine.MarkPhase2.
//
// Membership is the governing invariant applied per check — phase-2 iff the
// verdict depends on anything outside the doc+sidecar bytes:
//   - broken/ambiguous/canonicalize/link-resolve: resolve against __scanned_paths.
//   - property: resolves wikilinks against __scanned_paths + reads the git root.
//   - probe: executes md-run tasks; the finding IS the external exit code/timeout,
//     so a content-addressed cache would serve a stale verdict the moment the
//     probed system changes (opt-in, severity-off by default — recompute-every-run
//     is its current cost, no regression).
//   - effect-pin-resolves / effect-pin-on-origin / effect-checksum-reproduces /
//     effect-pin-stale: resolve pins against external git refs under
//     $CCC_LLM_WIKI_REPOS_ROOT (origin sha, ancestry, checksum), recomputed every
//     run over a per-run snapshot (plan §2).
//   - repo-cataloged / chain-fresh (U-B2b): verdicts depend on OTHER pages'
//     facts — the repo catalog, chain-dependency slice hashes, the effect-graph
//     edge set, ancestor blurb procedure blocks. Creating/editing/deleting a
//     dependency flips the verdict with this doc unchanged, so findings are
//     recomputed every run over the __all_facts corpus table and never cached.
//
// Doc-local checks are deliberately absent: table-wikilink-pipe (unescaped
// pipes in a row — no cross-file input) and wiki-navlink / backticked-wikilink
// (pure body grammar) stay phase-1 and remain cacheable. effect-unpinned is also
// phase-1: it is a pure function of the page's own frontmatter (is a pin present
// at all?), so it is correctly cacheable and NOT listed here.
var Phase2 = []string{
	"broken-wikilink",
	"ambiguous-wikilink",
	"wikilink-canonicalize",
	"link-resolve",
	"property",
	"probe",
	"effect-pin-resolves",
	"effect-pin-on-origin",
	"effect-checksum-reproduces",
	"effect-pin-stale",
	"repo-cataloged",
	"chain-fresh",
}
