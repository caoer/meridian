package engine

import (
	"io/fs"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
)

// inlineSuppressHTMLRe matches `<!-- md-disable-next-line rule-a, rule-b -->`.
var inlineSuppressHTMLRe = regexp.MustCompile(`<!--\s*md-disable-next-line\s+([^>]+?)\s*-->`)

// inlineSuppressObsidianRe matches `%% md-disable-next-line rule-a, rule-b %%`.
var inlineSuppressObsidianRe = regexp.MustCompile(`%%\s*md-disable-next-line\s+([^%]+?)\s*%%`)

// parseInlineSuppress scans body for `md-disable-next-line <rule>[, <rule>]` directives.
// Each directive on body line i suppresses listed rules on file line BodyOffset+i+2.
func parseInlineSuppress(body string, bodyOffset int) map[int]map[string]bool {
	if body == "" {
		return nil
	}
	out := make(map[int]map[string]bool)
	for i, line := range strings.Split(body, "\n") {
		var matches [][]string
		matches = append(matches, inlineSuppressHTMLRe.FindAllStringSubmatch(line, -1)...)
		matches = append(matches, inlineSuppressObsidianRe.FindAllStringSubmatch(line, -1)...)
		if len(matches) == 0 {
			continue
		}
		targetLine := bodyOffset + i + 2 // suppress applies to NEXT file line
		set := out[targetLine]
		if set == nil {
			set = make(map[string]bool)
			out[targetLine] = set
		}
		for _, m := range matches {
			for _, id := range strings.Split(m[1], ",") {
				id = strings.TrimSpace(id)
				if id != "" {
					set[id] = true
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Scan walks the filesystem and parses each .md file into a Document.
// Optional skip patterns cause directories with matching names to be skipped.
func Scan(fsys fs.FS, skip ...string) ([]*Document, error) {
	skipSet := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipSet[s] = true
	}

	var docs []*Document

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && skipSet[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil // skip unreadable files
		}

		doc := &Document{
			Path:        path,
			RawContent:  data,
			Frontmatter: make(map[string]any),
		}

		parsed, err := frontmatter.ParseBytes(data)
		if err != nil {
			// Invalid frontmatter — still include doc with empty meta
			docs = append(docs, doc)
			return nil
		}
		if parsed != nil {
			doc.Frontmatter = parsed.Meta
			doc.Tags = parsed.Tags()
			doc.Body = parsed.Body
			doc.BodyOffset = parsed.BodyOffset
			doc.LintIgnore = parsed.StringListField("lint-ignore")
		}
		doc.InlineSuppress = parseInlineSuppress(doc.Body, doc.BodyOffset)

		docs = append(docs, doc)
		return nil
	})

	return docs, err
}
