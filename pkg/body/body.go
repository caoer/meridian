// Package body is the public surface of meridian's markdown-as-filesystem engine —
// the byte-span, path-addressable view of a markdown record with a guarded
// splice-never-serialize write path. It is the package ccc-statusd imports (the
// first coupling between the two repos); everything here is a thin re-export of the
// implementation in meridian's internal/body, so the daemon links one engine rather
// than shelling out per read or decoding a private cache.
//
// The laws (span law, I0 splice-never-serialize, the byte-span/line-number
// convention, the sec_rev/file_rev contract) are documented on internal/body. This
// file only forwards. As of U1 the implementation is a compiling skeleton whose
// stubs return [ErrNotImpl]; U2/U3 fill them without changing this surface.
package body

import ibody "github.com/caoer/meridian/internal/body"

// ErrNotImpl is re-exported so callers of the public surface can distinguish
// "engine not built yet" (U1 skeleton) from a real failure via errors.Is.
var ErrNotImpl = ibody.ErrNotImpl

// Public types — aliases so methods (Document.Toc, Document.Read) and struct
// literals resolve identically across the internal and public packages.
type (
	// Document is an immutable, path-addressable view of one markdown record.
	Document = ibody.Document
	// Section is one addressable region (heading section or block).
	Section = ibody.Section
	// Toc is the shape of a document: its heading tree, no content.
	Toc = ibody.Toc
	// Edit is one guarded change applied by Splice.
	Edit = ibody.Edit
	// EditOp is the closed set of splice operations.
	EditOp = ibody.EditOp
	// Result is the outcome of a Splice.
	Result = ibody.Result
	// SectionDelta is one entry of a DiffSections result.
	SectionDelta = ibody.SectionDelta
	// DeltaKind classifies a SectionDelta.
	DeltaKind = ibody.DeltaKind
	// Error is the single structured engine error {code, message, remedy, context}.
	Error = ibody.Error
	// SpliceOption configures one Splice invocation (see Force).
	SpliceOption = ibody.SpliceOption
	// ForceStat aggregates one actor's journaled writes and forced-warning
	// overrides — the R-force audit surface.
	ForceStat = ibody.ForceStat
)

// EditOp values, re-exported.
const (
	OpReplace        = ibody.OpReplace
	OpAppend         = ibody.OpAppend
	OpReplaceSection = ibody.OpReplaceSection
	OpCreateSection  = ibody.OpCreateSection
	OpSetProperty    = ibody.OpSetProperty
)

// DeltaKind values, re-exported.
const (
	DeltaAdded    = ibody.DeltaAdded
	DeltaRemoved  = ibody.DeltaRemoved
	DeltaModified = ibody.DeltaModified
)

// Load reads path and returns its immutable Document, failing loud on malformed
// structure (content before frontmatter is an error).
func Load(path string) (*Document, error) { return ibody.Load(path) }

// Parse builds a Document from in-memory bytes under the same rules as Load.
func Parse(src []byte) (*Document, error) { return ibody.Parse(src) }

// Splice is the one write path: guarded, locked, atomic byte-splice over target.
func Splice(target string, edits []Edit, rev, actor string, opts ...SpliceOption) (Result, error) {
	return ibody.Splice(target, edits, rev, actor, opts...)
}

// Force opts a Splice into overriding WARNING-rung I4 conformance findings.
// Errors are never forceable; every forced warning is journaled and censused.
func Force() SpliceOption { return ibody.Force() }

// JournalForceStats returns per-actor force stats from the journal governing
// target — the per-agent force-rate surface (R-force).
func JournalForceStats(target string) map[string]ForceStat { return ibody.JournalForceStats(target) }

// ForceStatsUnder aggregates per-actor force stats from every journal under root
// — the fleet def-census consumer (R-force).
func ForceStatsUnder(root string) map[string]ForceStat { return ibody.ForceStatsUnder(root) }

// DiffSections reports how sections changed between two revisions of a document.
func DiffSections(a, b *Document) []SectionDelta { return ibody.DiffSections(a, b) }
