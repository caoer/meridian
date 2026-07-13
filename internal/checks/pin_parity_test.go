package checks

import (
	"reflect"
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// TestPinParity_ParsePinMatchesExtractPin pins the checks-side parsePin to the
// engine-side extractor. Since B2c the parity holds by CONSTRUCTION — parsePin
// delegates to engine.ExtractPin, the one pin parser — so this test is a slim
// regression guard that the delegation (and its shape classification) is not
// re-forked: field identity across the marker matrix, and the tri-state shape
// on each side of it. The parity is still load-bearing for concurrency: the
// parallel phase-2 pass batches every pin's git queries once in buildFromParams
// (from __all_pins) and relies on pinPreamble's r.ensure(pin) being a no-op
// because the pin's queries were already covered.
func TestPinParity_ParsePinMatchesExtractPin(t *testing.T) {
	cases := []struct {
		name  string
		fm    map[string]any
		shape engine.PinShape
	}{
		{"legacy full string-list", map[string]any{
			"repo": "pinned", "branch": "main", "commit": "abc123",
			"location": "pack/", "checksum": "deadbeef",
		}, engine.PinShapeLegacy},
		{"legacy full any-list", map[string]any{
			"repo": "pinned", "branch": "main", "commit": "abc123",
			"location": []any{"a/", "b.md"}, "checksum": []any{"s1", "s2"},
		}, engine.PinShapeLegacy},
		{"tombstone no markers", map[string]any{
			"repo": "pinned", "branch": "main",
		}, engine.PinShapeNone},
		{"whitespace padded", map[string]any{
			"repo": "  pinned  ", "branch": " main ", "commit": " abc123 ",
			"location": []any{"  a/  ", ""}, "checksum": []any{" s1 "},
		}, engine.PinShapeLegacy},
		{"non-string field types ignored", map[string]any{
			"repo": 42, "branch": true, "commit": "abc123",
			"location": []any{"a", 7, "b"}, "checksum": "s",
		}, engine.PinShapeLegacy},
		{"empty commit no inputs is unpinned", map[string]any{
			"repo": "pinned", "branch": "main", "commit": "",
			"location": "a/", "checksum": "s",
		}, engine.PinShapeNone},
		{"receipt shape: inputs pointer, no commit", map[string]any{
			"repo": "pinned", "location": "a/",
			"inputs": "[[#^inputs]]", "receipt": nil,
		}, engine.PinShapeReceipt},
		{"transitional: both markers", map[string]any{
			"repo": "pinned", "branch": "main", "commit": "abc123",
			"location": "a/", "checksum": "s",
			"inputs": "[[#^inputs]]",
		}, engine.PinShapeTransitional},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pin, shape, _ := parsePin(pinDoc(c.fm))
			factsPin := engine.ExtractFacts(pinDoc(c.fm)).Pin

			if shape != c.shape {
				t.Fatalf("parsePin shape = %v, want %v", shape, c.shape)
			}
			if factsPin.Shape() != c.shape {
				t.Fatalf("ExtractFacts pin shape = %v, want %v", factsPin.Shape(), c.shape)
			}
			if (shape == engine.PinShapeNone) != (factsPin == nil) {
				t.Fatalf("presence disagreement: parsePin shape=%v, extract pin nil=%v", shape, factsPin == nil)
			}
			if shape == engine.PinShapeNone {
				return
			}
			if !reflect.DeepEqual(pin, effectPinFromFields(*factsPin)) {
				t.Fatalf("field drift: parsePin=%+v extract=%+v", pin, *factsPin)
			}
		})
	}
}
