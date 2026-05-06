package testkit

import (
	"bytes"

	"github.com/caoer/meridian/internal/vfs"
	"go.yaml.in/yaml/v3"
)

// Entry is something added to a test wiki.
type Entry struct {
	path    string
	content string
	isDir   bool
}

// F creates a file entry.
func F(path, content string) Entry {
	return Entry{path: path, content: content}
}

// D creates a directory entry.
func D(path string) Entry {
	return Entry{path: path, isDir: true}
}

// FM creates a file with generated frontmatter + body.
func FM(path string, meta map[string]any, body string) Entry {
	var buf bytes.Buffer
	buf.WriteString("---\n")
	yamlBytes, _ := yaml.Marshal(meta)
	buf.Write(yamlBytes)
	buf.WriteString("---\n")
	buf.WriteString(body)
	return Entry{path: path, content: buf.String()}
}

// Wiki creates an in-memory filesystem from entries.
func Wiki(entries ...Entry) *vfs.MemFS {
	f := vfs.NewMemFS()
	for _, e := range entries {
		if e.isDir {
			f.MkdirAll(e.path, 0755)
		} else {
			f.AddFile(e.path, e.content)
		}
	}
	return f
}
