// Package checks provides built-in check implementations for meridian.
// Stage 1 workers: add new checks as separate files in this package,
// then register them in the All map.
package checks

import "github.com/caoer/meridian/internal/engine"

// All is the registry of built-in checks. Each entry maps a check name
// (as referenced by rule YAML "check" field) to its implementation.
var All = map[string]engine.CheckFunc{
	"field-exists": fieldExistsCheck,
	"tag-format":   tagFormatCheck,
	"pattern":      patternCheck,
}
