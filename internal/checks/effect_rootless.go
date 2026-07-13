package checks

import (
	"fmt"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/run"
)

// effectRootlessCheck: an effect must declare what it derives from. A page
// tagged type/effect must carry at least one `inputs:` entry naming a
// dependency; a self-ref (`[[#Section]]`) counts, so a self-rooted effect is
// legal. A page with no inputs is "rootless" — nothing records what it was
// built from, so the attestation chain has no anchor.
//
// Tag-scoped on parsed frontmatter tags, never on path or body text (mirrors
// effect-unpinned) — EFFECTS.md (type/index) and colocated artifact content
// under effects/ stay silent by construction. Tombstones (status: retired |
// pending) are silent: an un-authored or retired page has nothing to root.
//
// PURE (frontmatter only — no git, no corpus) → phase-1, cacheable. It must not
// touch engine-injected corpus params.
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

	entries, ok := doc.Frontmatter["inputs"].([]any)
	if !ok || len(entries) == 0 {
		return []engine.RawFinding{{
			Line: 1,
			TemplateData: map[string]string{
				"Reason": "type/effect page has no inputs — declare at least one inputs entry (a [[#Section]] self-ref roots a self-derived effect), or declare status: retired|pending",
			},
		}}
	}

	// Presence satisfies rootless; each entry must still be a well-formed
	// wikilink ref (strict writer — a malformed input is a finding, not a pass).
	var out []engine.RawFinding
	for i, e := range entries {
		m, ok := e.(map[string]any)
		if !ok {
			out = append(out, malformedInputFinding(i, fmt.Sprintf("entry is %T, want a mapping with a `ref`", e)))
			continue
		}
		refRaw, present := m["ref"]
		if !present {
			out = append(out, malformedInputFinding(i, "entry has no `ref`"))
			continue
		}
		ref, ok := refRaw.(string)
		if !ok {
			out = append(out, malformedInputFinding(i, fmt.Sprintf("`ref` is %T, want a wikilink string", refRaw)))
			continue
		}
		if _, err := run.ParseWikilink(ref); err != nil {
			out = append(out, malformedInputFinding(i, fmt.Sprintf("`ref` %q is not a wikilink", ref)))
			continue
		}
	}
	return out
}

func malformedInputFinding(i int, why string) engine.RawFinding {
	return engine.RawFinding{
		Line: 1,
		TemplateData: map[string]string{
			"Reason": fmt.Sprintf("malformed inputs[%d]: %s", i, why),
		},
	}
}
