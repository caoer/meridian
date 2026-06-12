package run

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// ErrAmbiguous marks an expect-unique violation (link resolved to >1 note).
var ErrAmbiguous = errors.New("ambiguous target")

// ReadMatch is one resolved document (or extracted fragment).
type ReadMatch struct {
	Path    string
	Content string
}

// ReadResult is the payload of `md read`: the resolution base and every
// matched path are always reported so automation can verify what was read.
type ReadResult struct {
	Base     string
	Target   string
	Matches  []ReadMatch
	Warnings []string
}

// Read resolves a target — a relative path or an Obsidian wikilink
// ([[note]], [[note#Heading]], [[note#^id]]) — against fsys (rooted at base)
// and returns the matched content. Multiple wikilink matches all render
// (exit 0) unless expectUnique is set.
func Read(fsys fs.FS, base, target string, expectUnique bool) (ReadResult, error) {
	res := ReadResult{Base: base, Target: target}
	target = strings.TrimSpace(target)
	if target == "" {
		return res, fmt.Errorf("empty target")
	}

	// Plain path target → whole file. Resolution is rooted at the cwd, so
	// only cwd-relative paths are resolvable.
	if !strings.HasPrefix(target, "[[") {
		rel := path.Clean(target)
		if path.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
			return res, fmt.Errorf("target %q must be a cwd-relative path — absolute and ../ paths are not resolvable (resolution is rooted at %s)", target, base)
		}
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			return res, fmt.Errorf("read %s: %w", rel, err)
		}
		res.Matches = []ReadMatch{{Path: rel, Content: string(data)}}
		return res, nil
	}

	link, err := ParseWikilink(target)
	if err != nil {
		return res, err
	}
	if link.Target == "" {
		return res, fmt.Errorf("same-file link %q has no file context in md read — use [[note#...]]", target)
	}

	paths, err := ResolveNote(fsys, link.Target)
	if err != nil {
		return res, fmt.Errorf("%w (resolution base: %s)", err, base)
	}
	if expectUnique && len(paths) > 1 {
		return res, fmt.Errorf("%w: %q resolves to %d notes: %s",
			ErrAmbiguous, target, len(paths), strings.Join(paths, ", "))
	}

	for _, p := range paths {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		content := string(data)
		switch {
		case link.BlockID != "":
			b, err := FindBlock(content, link.BlockID)
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", p, err))
				continue
			}
			content = b.Raw
		case link.Heading != "":
			s, err := Section(content, link.Heading)
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", p, err))
				continue
			}
			content = s
		}
		res.Matches = append(res.Matches, ReadMatch{Path: p, Content: content})
	}

	if len(res.Matches) == 0 {
		return res, fmt.Errorf("%q matched %d note(s) but no content resolved: %s",
			target, len(paths), strings.Join(res.Warnings, "; "))
	}
	return res, nil
}
