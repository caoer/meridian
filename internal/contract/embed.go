// Package contract provides the embedded contract rule pack.
// The 9 YAML files under rules/contract/ are compiled into the binary
// via go:embed and served as the fallback when no filesystem pack is
// configured or found.
package contract

import (
	"embed"
	"io/fs"
)

//go:embed rules/*.yaml
var embedded embed.FS

// FS returns an fs.FS rooted at the rules/ subdirectory inside the
// embedded filesystem, so callers see the YAML files directly.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "rules")
	if err != nil {
		// Cannot happen: "rules" is a compile-time directory.
		panic("contract: embedded rules subdirectory missing: " + err.Error())
	}
	return sub
}
