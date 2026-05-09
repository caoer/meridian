package engine

import (
	"io/fs"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
)

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

		docs = append(docs, doc)
		return nil
	})

	return docs, err
}
