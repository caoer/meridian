package mv

import (
	"io/fs"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// Relint runs engine checks on the specified files at their new locations.
// Returns findings as warnings (informational — not blocking the move).
// Only returns findings for paths in the provided list.
func Relint(fsys fs.FS, eng *engine.Engine, ruleList []rules.Rule, paths []string) []types.Finding {
	all := eng.Run(fsys, ruleList)

	// Build set of moved paths for fast lookup
	moved := make(map[string]bool, len(paths))
	for _, p := range paths {
		moved[p] = true
	}

	var filtered []types.Finding
	for _, f := range all {
		if moved[f.FilePath] {
			filtered = append(filtered, f)
		}
	}

	return filtered
}
