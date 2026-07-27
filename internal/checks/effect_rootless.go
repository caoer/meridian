package checks

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/run"
)

// effectRootlessCheck: an effect must declare what it derives from — its
// attestation chain must be non-empty. Per the ratified receipt schema
// (effect-attestation-refounding §5.1 "The chain — hashed inputs" / SCHEMA
// "The chain block — ^inputs"), frontmatter carries the unconditional pointer
// `inputs: '[[#^inputs]]'` and the chain itself is a fenced YAML block anchored
// ^inputs, each item a `- ref:` dependency. A self-ref (`[[#Section]]`) counts,
// so a self-rooted effect is legal. A page whose chain is empty — no `inputs:`
// pointer, or a pointer resolving to a missing / item-less ^inputs block — is
// "rootless": nothing records what it was built from, so the attestation chain
// has no anchor.
//
// Scope is non-emptiness only ("`inputs` MUST be non-empty", SCHEMA § chain):
// per-item ref resolution and hash validity are the chain-hash / graph rules'
// job. This check owns the one rootless invariant, nothing more — which is why
// it does not YAML-parse the block (the ratified block is a sequence plus a
// trailing `hash-algo:` scalar, not a single valid YAML document; it is counted
// by top-level sequence items, never unmarshalled).
//
// Tag-scoped on parsed frontmatter tags, never on path or body text (mirrors
// effect-unpinned) — EFFECTS.md (type/index) and colocated artifact content
// under effects/ stay silent by construction. Tombstones (status: retired |
// pending) are silent: an un-authored or retired page has nothing to root.
//
// PURE per-doc — frontmatter plus the doc's OWN ^inputs body block (no git, no
// corpus, no engine-injected params; the same phase-1 discipline as ExtractPin's
// receipt-block read). It must not touch engine-injected corpus params.
func effectRootlessCheck(doc *engine.Document, _ map[string]any) []engine.RawFinding {
	isEffect := false
	for _, tag := range doc.Tags {
		if tag == "type/effect" {
			isEffect = true
			break
		}
	}
	if !isEffect {
		return nil
	}
	if status, _ := doc.Frontmatter["status"].(string); status == "retired" || status == "pending" {
		return nil
	}

	raw, present := doc.Frontmatter["inputs"]
	if !present {
		return rootlessFinding("type/effect page has no inputs — declare the chain pointer `inputs: '[[#^inputs]]'` with a non-empty ^inputs block (a [[#Section]] self-ref roots a self-derived effect), or declare status: retired|pending")
	}

	// Ratified schema: `inputs` is the unconditional same-page block pointer.
	// Resolve it against the doc's own body and require a non-empty chain.
	ptr, ok := raw.(string)
	if !ok {
		return rootlessFinding("inputs must be the block-pointer string `'[[#^inputs]]'` (receipt schema) — a frontmatter list is the pre-refounding shape and no longer roots an effect")
	}
	wl, err := run.ParseWikilink(ptr)
	if err != nil || wl.Target != "" || wl.BlockID == "" {
		return rootlessFinding(fmt.Sprintf("inputs pointer %q is not a same-page block ref `'[[#^inputs]]'` (receipt schema)", ptr))
	}
	blk, err := run.FindBlock(string(doc.RawContent), wl.BlockID)
	if err != nil {
		return rootlessFinding(fmt.Sprintf("inputs pointer resolves to no ^%s block — the attestation chain has no anchor", wl.BlockID))
	}
	if chainItemCount(blk) == 0 {
		return rootlessFinding(fmt.Sprintf("^%s block declares no chain items — an effect must derive from at least one input", wl.BlockID))
	}
	return nil
}

// chainSeqItem matches a top-level YAML sequence item (`-` at column 0), the
// per-item marker of the ^inputs chain. Indented continuation lines (`  hash:`),
// trailing block-level scalars (`hash-algo:`), and comments are not items.
var chainSeqItem = regexp.MustCompile(`^-(\s|$)`)

// chainItemCount counts top-level sequence items in a resolved ^inputs block —
// robust to the block's trailing `hash-algo:` scalar (which makes the block an
// invalid single YAML document, so it is line-counted, never unmarshalled).
func chainItemCount(blk run.Block) int {
	body := blk.Code // fenced-block inner content (the ratified shape)
	if !blk.Fence {
		body = blk.Raw
	}
	n := 0
	for _, line := range strings.Split(body, "\n") {
		if chainSeqItem.MatchString(line) {
			n++
		}
	}
	return n
}

func rootlessFinding(reason string) []engine.RawFinding {
	return []engine.RawFinding{{
		Line:         1,
		TemplateData: map[string]string{"Reason": reason},
	}}
}
