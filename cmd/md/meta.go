package main

// Shared frontmatter-value coercions for the cmd/md handlers that read parsed
// YAML (map[string]any) — run records, status exec-facts, walk exec-facts. One
// home so the surface verbs read a scalar the same way (dedupe of the identical
// helpers that lived in status_handler.go and walk_handler.go).

func metaStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func metaInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func metaBool(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}
