package mv

import (
	"io/fs"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// Relint runs engine checks on the specified files at their new locations.
// Returns findings as warnings (informational — not blocking the move).
// Only parses and evaluates the specified paths, walking the tree cheaply
// for cross-file context (wikilink resolution).
func Relint(fsys fs.FS, eng *engine.Engine, ruleList []rules.Rule, paths []string) []types.Finding {
	return eng.RunForPaths(fsys, ruleList, paths)
}
