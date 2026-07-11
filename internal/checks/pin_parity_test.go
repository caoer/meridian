package checks

import (
	"reflect"
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// TestPinParity_ParsePinMatchesExtractPin pins the two frontmatter pin parsers to
// identical field output: checks.parsePin (used by the phase-2 checks) and
// engine.extractPin (via ExtractFacts, used to build the __all_pins corpus batch).
//
// This parity is load-bearing for correctness AND concurrency. The parallel
// phase-2 pass batches every pin's git queries once in buildFromParams (from
// __all_pins) and relies on pinPreamble's r.ensure(pin) being a no-op because the
// pin's queries were already covered. That holds only if extractPin and parsePin
// derive identical Repo/Branch/Commit/Locations from the same frontmatter — if
// they drift, a check evaluates a pin whose queries were never batched, ensure()
// writes repoData.objs while other workers read it lock-free, and that is a real
// data race. The two parsers live in different packages (engine cannot import
// checks) with duplicated logic; this test is the only thing pinning them.
func TestPinParity_ParsePinMatchesExtractPin(t *testing.T) {
	cases := []struct {
		name string
		fm   map[string]any
	}{
		{"full string-list", map[string]any{
			"repo": "pinned", "branch": "main", "commit": "abc123",
			"location": "pack/", "checksum": "deadbeef",
		}},
		{"full any-list", map[string]any{
			"repo": "pinned", "branch": "main", "commit": "abc123",
			"location": []any{"a/", "b.md"}, "checksum": []any{"s1", "s2"},
		}},
		{"tombstone no commit", map[string]any{
			"repo": "pinned", "branch": "main",
		}},
		{"whitespace padded", map[string]any{
			"repo": "  pinned  ", "branch": " main ", "commit": " abc123 ",
			"location": []any{"  a/  ", ""}, "checksum": []any{" s1 "},
		}},
		{"non-string field types ignored", map[string]any{
			"repo": 42, "branch": true, "commit": "abc123",
			"location": []any{"a", 7, "b"}, "checksum": "s",
		}},
		{"empty commit is unpinned", map[string]any{
			"repo": "pinned", "branch": "main", "commit": "",
			"location": "a/", "checksum": "s",
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pin, present, _ := parsePin(pinDoc(c.fm))
			factsPin := engine.ExtractFacts(pinDoc(c.fm)).Pin

			if present != (factsPin != nil) {
				t.Fatalf("presence disagreement: parsePin present=%v, extractPin nil=%v", present, factsPin == nil)
			}
			if !present {
				return
			}
			got := effectPinFromFields(*factsPin)
			if pin.Repo != got.Repo || pin.Branch != got.Branch || pin.Commit != got.Commit {
				t.Fatalf("scalar drift: parsePin=%+v extractPin=%+v", pin, got)
			}
			if !reflect.DeepEqual(pin.Locations, got.Locations) {
				t.Fatalf("location drift: parsePin=%v extractPin=%v", pin.Locations, got.Locations)
			}
			if !reflect.DeepEqual(pin.Checksums, got.Checksums) {
				t.Fatalf("checksum drift: parsePin=%v extractPin=%v", pin.Checksums, got.Checksums)
			}
		})
	}
}
