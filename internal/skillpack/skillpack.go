// Package skillpack ships the embedded lint pack for skill trees — shipped
// skill directories (SKILL.md + references + setup/) that live outside any
// wiki: no meridian.yaml, no rule pack, so ordinary `md check` has nothing
// to run there (R9 detector-gap #2). The pack is wikilink integrity only,
// resolved self-contained within the tree — a skill's dangling reference
// IS the drift signal.
package skillpack

import (
	"embed"
	"fmt"

	"github.com/caoer/meridian/internal/rules"
)

//go:embed pack
var packFS embed.FS

// Rules returns the embedded skill-tree pack.
func Rules() ([]rules.Rule, error) {
	rs, _, err := rules.LoadFS(packFS, "pack")
	if err != nil {
		return nil, fmt.Errorf("embedded skill-tree pack: %w", err)
	}
	return rs, nil
}
