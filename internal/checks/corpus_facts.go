package checks

import (
	"io/fs"

	"github.com/caoer/meridian/internal/engine"
)

// corpusFacts returns the corpus fact table (path → phase-1 facts) for a
// phase-2 corpus check. On a full run the engine injects it as __all_facts —
// cache-served, zero extra parses (runPhase2). On a scoped run (RunForPaths:
// md fix / md mv / watch incremental) the engine parses only the target docs,
// so the table is rebuilt here from __fs + __scanned_paths, memoized per run in
// __index_cache — a scoped run must degrade to a slower verdict, never to a
// silently emptier one (the false-clean class).
func corpusFacts(params map[string]any) map[string]engine.Facts {
	if table, ok := params["__all_facts"].(map[string]engine.Facts); ok {
		return table
	}
	v := indexCacheGetOrBuild(params, "__corpus_facts_fallback", func() any {
		fsys, ok := params["__fs"].(fs.FS)
		if !ok {
			return map[string]engine.Facts{}
		}
		paths, _ := params["__scanned_paths"].([]string)
		docs, err := engine.ScanFiles(fsys, paths, 0)
		if err != nil {
			return map[string]engine.Facts{}
		}
		table := make(map[string]engine.Facts, len(docs))
		for _, d := range docs {
			table[d.Path] = engine.ExtractFacts(d)
		}
		return table
	})
	table, _ := v.(map[string]engine.Facts)
	return table
}
