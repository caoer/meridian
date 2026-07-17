// cascade.go resolves the def for a kind across the layer ladder — session →
// preset → builtin — with NEAREST-WINS-PER-KEY merging (schema-v2 §1.3):
// property-level for ^properties, name-level for section rules, whole-block for
// the template. There is no extends: chain to depth-limit; per-key resolution
// over an ordered dir list cannot form a diamond.
package defs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoDef marks a Resolve miss: no def file exists for the kind anywhere in the
// layer ladder. Callers that must FAIL CLOSED (def check, def fix) treat it like
// any resolve error; the WRITE-path hook (I4) distinguishes it from a malformed
// def — a kind nobody defined is not a record contract, so the write proceeds,
// while a def that EXISTS but fails to load refuses-unless-forced.
var ErrNoDef = errors.New("no def resolves for kind")

// Resolve merges the def for kind (and optional preset) across layers — an
// ordered list of defs directories, NEAREST FIRST. Within one layer a
// preset-qualified file (<kind>-<preset>.md) is nearer than the generic
// <kind>.md. No def found anywhere is an error: the validator fails closed
// rather than validating against an imagined schema.
func Resolve(kind, preset string, layers []string) (*Def, error) {
	var merged *Def
	for _, dir := range layers {
		names := []string{kind + ".md"}
		if preset != "" {
			names = append([]string{kind + "-" + preset + ".md"}, names...)
		}
		for _, name := range names {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err != nil {
				continue
			}
			def, err := LoadDef(path)
			if err != nil {
				return nil, err // malformed def anywhere in the cascade → fail closed
			}
			if def.Kind != kind {
				return nil, fmt.Errorf("def %s: defines %q, resolved for kind %q", path, def.Kind, kind)
			}
			merged = mergeDef(merged, def)
		}
	}
	if merged == nil {
		return nil, fmt.Errorf("%w: kind %q (preset %q) in layers %v", ErrNoDef, kind, preset, layers)
	}
	return merged, nil
}

// mergeDef folds a FARTHER def under an already-merged NEARER one: a key the
// nearer def declares wins; keys only the farther def declares fill in.
func mergeDef(near, far *Def) *Def {
	if near == nil {
		return far
	}
	for key, spec := range far.Props {
		if _, taken := near.Props[key]; !taken {
			near.Props[key] = spec
		}
	}
	for _, fs := range far.Sections {
		if _, taken := near.Section(fs.Name); !taken {
			near.Sections = append(near.Sections, fs)
		}
	}
	if len(near.Template) == 0 {
		near.Template = far.Template
	}
	if near.Version == 0 {
		near.Version = far.Version
	}
	near.Sources = append(near.Sources, far.Sources...)
	return near
}

// ResolveLayer resolves the def for a kind (and optional preset) from ONE
// virtual layer — a name → content lookup instead of directories on disk. The
// pipe's in-sandbox def-check resolves this way from the fabric's types/
// projection: the same nearest-wins merging as Resolve applies within the
// layer (the preset-qualified def is nearer than the generic one), but no
// os.Stat over the host tree and no $UCC_HOME — resolution is a pure function
// of the lookup, so a sandboxed program can neither probe the host filesystem
// nor detect the environment through def resolution, and error text carries
// only the names the lookup was asked for, never a host absolute path.
func ResolveLayer(kind, preset string, load func(name string) []byte) (*Def, error) {
	var merged *Def
	names := []string{kind + ".md"}
	if preset != "" {
		names = append([]string{kind + "-" + preset + ".md"}, names...)
	}
	for _, name := range names {
		raw := load(name)
		if raw == nil {
			continue
		}
		def, err := ParseDef(raw, name)
		if err != nil {
			return nil, err // malformed def anywhere in the cascade → fail closed
		}
		if def.Kind != kind {
			return nil, fmt.Errorf("def %s: defines %q, resolved for kind %q", name, def.Kind, kind)
		}
		merged = mergeDef(merged, def)
	}
	if merged == nil {
		return nil, fmt.Errorf("%w: kind %q (preset %q)", ErrNoDef, kind, preset)
	}
	return merged, nil
}

// DiscoverLayers builds the default layer ladder for a record path: every
// defs/ directory from the record's own directory upward (session layer,
// nearest first), then $UCC_HOME/defs (builtin). An explicit layer list from
// the caller always replaces this.
func DiscoverLayers(recordPath string) []string {
	var layers []string
	dir, err := filepath.Abs(filepath.Dir(recordPath))
	if err != nil {
		dir = filepath.Dir(recordPath)
	}
	for {
		cand := filepath.Join(dir, "defs")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			layers = append(layers, cand)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if home := os.Getenv("UCC_HOME"); home != "" {
		cand := filepath.Join(home, "defs")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			layers = append(layers, cand)
		}
	}
	return layers
}
