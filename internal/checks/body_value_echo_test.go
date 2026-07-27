package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

const fullSha = "6603b42e1a2b3c4d5e6f70819293a4b5c6d7e8f9"     // 40 hex
const checksumSha = "abcdef0123456789abcdef0123456789abcdef01" // 40 hex

func echoDoc(fm map[string]any, body string) *engine.Document {
	return &engine.Document{Path: "effects/skills/x.md", Frontmatter: fm, Body: body, BodyOffset: 4}
}

func TestBodyValueEcho(t *testing.T) {
	pin := func() map[string]any {
		return map[string]any{"commit": fullSha, "checksum": checksumSha}
	}

	cases := []struct {
		name   string
		doc    *engine.Document
		params map[string]any
		want   int
	}{
		{"clean body — no echo", echoDoc(pin(),
			"This effect derives from the architecture doc. No shas here."), nil, 0},
		{"full commit echoed in prose", echoDoc(pin(),
			"pinned at "+fullSha+" today"), nil, 1},
		{"short-sha reference echoed (>=7 prefix)", echoDoc(pin(),
			"see changelog 6603b42e for the applied change"), nil, 1},
		{"checksum echoed", echoDoc(pin(),
			"blob "+checksumSha+" reproduces"), nil, 1},
		{"echo inside a fenced code block is NOT exempt", echoDoc(pin(),
			"```\ncommit "+fullSha+"\n```"), nil, 1},
		{"echo inside inline code is NOT exempt", echoDoc(pin(),
			"the pin `6603b42e` is wrong here"), nil, 1},
		{"no pin fields — nothing to echo", echoDoc(
			map[string]any{"repo": "home-wiki"}, "6603b42e appears but is not this page's pin"), nil, 0},
		{"non-hex field value skipped", echoDoc(
			map[string]any{"commit": "main"}, "the main branch"), nil, 0},
		{"sub-7-hex run not matched", echoDoc(
			map[string]any{"commit": "abc1234def5678"}, "id abc123 is short"), nil, 0},
		{"fields override guards only commit", echoDoc(pin(),
			"blob "+checksumSha+" but no commit"),
			map[string]any{"fields": []any{"commit"}}, 0},
		{"fail-closed: empty fields list is a finding", echoDoc(pin(), "anything"),
			map[string]any{"fields": []any{}}, 1},
		{"fail-closed: non-list fields is a finding", echoDoc(pin(), "anything"),
			map[string]any{"fields": 5}, 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := bodyValueEchoCheck(c.doc, c.params)
			if len(got) != c.want {
				t.Fatalf("got %d findings (%v), want %d", len(got), got, c.want)
			}
		})
	}
}

// TestBodyValueEcho_LineNumber pins the body-relative line mapping: an echo on
// the second body line reports file line BodyOffset+1 (BodyOffset IS the first
// body line).
func TestBodyValueEcho_LineNumber(t *testing.T) {
	doc := echoDoc(map[string]any{"commit": fullSha},
		"first line clean\ncommit "+fullSha+" here")
	got := bodyValueEchoCheck(doc, nil)
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if want := doc.BodyOffset + 1; got[0].Line != want {
		t.Errorf("Line = %d, want %d (BodyOffset %d + 2nd body line)", got[0].Line, want, doc.BodyOffset)
	}
}

// TestBodyValueEcho_CaseInsensitive: git shas are lowercase but a page may
// uppercase them; hex compares case-insensitively.
func TestBodyValueEcho_CaseInsensitive(t *testing.T) {
	doc := echoDoc(map[string]any{"commit": fullSha}, "PIN 6603B42E uppercased")
	if got := bodyValueEchoCheck(doc, nil); len(got) != 1 {
		t.Fatalf("uppercase short-sha echo: got %d findings, want 1", len(got))
	}
}
