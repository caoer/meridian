package checks

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// effectDoc builds a type/effect Document. fm is the parsed frontmatter; body is
// the markdown after frontmatter (carrying any ^inputs block the check resolves
// via doc.RawContent). Tags mirror what the engine would have parsed.
func effectDoc(tags []string, fm map[string]any, body string) *engine.Document {
	return &engine.Document{
		Path:        "effects/skills/x.md",
		Tags:        tags,
		Frontmatter: fm,
		RawContent:  []byte(body),
	}
}

// receiptBody renders the ^inputs chain block per the ratified receipt schema
// (effect-attestation-refounding §5.1 / SCHEMA "The chain block — ^inputs"): a
// fenced YAML sequence of `- ref:` items plus the trailing block-level
// `hash-algo:` scalar — a sequence + scalar, NOT a single valid YAML document,
// by design. Zero refs renders an item-less block (a migrated-but-rootless page).
func receiptBody(refs ...string) string {
	var b strings.Builder
	b.WriteString("# migrated effect\n\n## Chain\n\n```yaml\n")
	for _, r := range refs {
		b.WriteString("- ref: '" + r + "'\n  hash: null\n")
	}
	b.WriteString("hash-algo: v1\n```\n^inputs\n")
	return b.String()
}

// receiptFM is the ratified receipt-shape frontmatter: `inputs` as the
// unconditional same-page block pointer.
func receiptFM() map[string]any {
	return map[string]any{"tags": []any{"type/effect"}, "inputs": "[[#^inputs]]"}
}

func TestEffectRootless(t *testing.T) {
	effect := []string{"domain/x", "type/effect"}

	cases := []struct {
		name string
		doc  *engine.Document
		want int
	}{
		// --- ratified receipt shape: pointer + non-empty ^inputs block passes ---
		// (the reconcile: these schema-shaped pages false-warned on pre-fix code
		//  that read `inputs` as a frontmatter list)
		{"schema-shaped page passes (pointer + chain item)",
			effectDoc(effect, receiptFM(), receiptBody("[[architecture#Rule System]]")), 0},
		{"self-rooted section ref passes",
			effectDoc(effect, receiptFM(), receiptBody("[[#Section]]")), 0},
		{"self-rooted block ref passes",
			effectDoc(effect, receiptFM(), receiptBody("[[#^anchor]]")), 0},
		{"multiple chain items pass",
			effectDoc(effect, receiptFM(), receiptBody("[[a#H]]", "[[b]]")), 0},

		// --- genuinely rootless (the real pre-migration behavior — unchanged) ---
		{"no inputs key, no block, is rootless",
			effectDoc(effect, map[string]any{"tags": []any{"type/effect"}},
				"# effect\n\nno chain here\n"), 1},
		{"pointer present but ^inputs block missing is rootless",
			effectDoc(effect, receiptFM(),
				"# migrated effect\n\n(chain block not authored)\n"), 1},
		{"pointer present but chain has zero items is rootless",
			effectDoc(effect, receiptFM(), receiptBody()), 1},

		// --- tombstones + non-effect stay silent ---
		{"retired tombstone silent",
			effectDoc(effect, map[string]any{"status": "retired"}, ""), 0},
		{"pending tombstone silent",
			effectDoc(effect, map[string]any{"status": "pending"}, ""), 0},
		{"non-effect page silent even with no inputs",
			effectDoc([]string{"domain/x", "type/reference"}, map[string]any{}, ""), 0},

		// --- pre-refounding shapes no longer root an effect (contract admits only
		//     the pointer + ^inputs block) ---
		{"frontmatter-list inputs (pre-refounding shape) is rootless",
			effectDoc(effect, map[string]any{"tags": []any{"type/effect"},
				"inputs": []any{map[string]any{"ref": "[[#Section]]"}}}, ""), 1},
		{"non-pointer inputs string is rootless",
			effectDoc(effect, map[string]any{"tags": []any{"type/effect"},
				"inputs": "architecture#Rule System"}, ""), 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := effectRootlessCheck(c.doc, nil)
			if len(got) != c.want {
				t.Fatalf("got %d findings (%v), want %d", len(got), got, c.want)
			}
		})
	}
}

// TestEffectRootless_PureNoCorpusParams asserts the phase-1 boundary: the check
// never reads engine-injected corpus params (a nil params map must not panic and
// must not change the verdict).
func TestEffectRootless_PureNoCorpusParams(t *testing.T) {
	doc := effectDoc([]string{"type/effect"}, map[string]any{"tags": []any{"type/effect"}}, "")
	got := effectRootlessCheck(doc, nil)
	if len(got) != 1 {
		t.Fatalf("nil-params run: got %d findings, want 1 (rootless)", len(got))
	}
	if !strings.Contains(got[0].TemplateData["Reason"], "no inputs") {
		t.Errorf("reason = %q, want a 'no inputs' rootless message", got[0].TemplateData["Reason"])
	}
}
