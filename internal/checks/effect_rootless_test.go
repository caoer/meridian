package checks

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// effectDoc builds a type/effect Document with the given frontmatter (the Tags
// slice is set from the tag list the engine would have parsed).
func effectDoc(tags []string, fm map[string]any) *engine.Document {
	return &engine.Document{Path: "effects/skills/x.md", Tags: tags, Frontmatter: fm}
}

func TestEffectRootless(t *testing.T) {
	effect := []string{"domain/x", "type/effect"}

	inputs := func(entries ...any) map[string]any {
		return map[string]any{"tags": []any{"type/effect"}, "inputs": entries}
	}

	cases := []struct {
		name string
		doc  *engine.Document
		want int
	}{
		{"self-rooted passes", effectDoc(effect,
			inputs(map[string]any{"ref": "[[#Section]]"})), 0},
		{"self-rooted block ref passes", effectDoc(effect,
			inputs(map[string]any{"ref": "[[#^anchor]]"})), 0},
		{"pointed ref passes", effectDoc(effect,
			inputs(map[string]any{"ref": "[[architecture#Rule System]]"})), 0},
		{"multiple valid refs pass", effectDoc(effect,
			inputs(map[string]any{"ref": "[[a#H]]"}, map[string]any{"ref": "[[b]]"})), 0},

		{"no inputs field fails (rootless)", effectDoc(effect,
			map[string]any{"tags": []any{"type/effect"}}), 1},
		{"empty inputs list fails (rootless)", effectDoc(effect,
			inputs()), 1},

		{"retired tombstone silent", effectDoc(effect,
			map[string]any{"status": "retired"}), 0},
		{"pending tombstone silent", effectDoc(effect,
			map[string]any{"status": "pending"}), 0},

		{"non-effect page silent even with no inputs", effectDoc(
			[]string{"domain/x", "type/reference"}, map[string]any{}), 0},

		{"malformed: entry missing ref", effectDoc(effect,
			inputs(map[string]any{"hash": "sha256:abc"})), 1},
		{"malformed: non-wikilink ref", effectDoc(effect,
			inputs(map[string]any{"ref": "architecture#Rule System"})), 1},
		{"malformed: non-string ref", effectDoc(effect,
			inputs(map[string]any{"ref": 5})), 1},
		{"malformed: bare-string entry (not a mapping)", effectDoc(effect,
			inputs("[[#Section]]")), 1},

		{"mixed valid + malformed reports only the malformed", effectDoc(effect,
			inputs(map[string]any{"ref": "[[a#H]]"}, map[string]any{"ref": "nope"})), 1},
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
// never reads engine-injected corpus params (a nil params map must not panic
// and must not change the verdict).
func TestEffectRootless_PureNoCorpusParams(t *testing.T) {
	doc := effectDoc([]string{"type/effect"}, map[string]any{"tags": []any{"type/effect"}})
	got := effectRootlessCheck(doc, nil)
	if len(got) != 1 {
		t.Fatalf("nil-params run: got %d findings, want 1 (rootless)", len(got))
	}
	if !strings.Contains(got[0].TemplateData["Reason"], "no inputs") {
		t.Errorf("reason = %q, want a 'no inputs' rootless message", got[0].TemplateData["Reason"])
	}
}
