// stale-run-record: a document's sidecar run record (<stem>.runs.md, written
// by `md run` record:true) stores the git blob object id of each task's block
// as it was when the task last ran. When the block's current content hashes
// differently, the recorded output predates the code — the notebook's
// "stale output cell" semantic, same staleness idea as the effect pin
// contract. Records are opt-in: no sidecar, no findings. Two issues:
//
//	stale          recorded block_sha ≠ current block content hash
//	never recorded sidecar exists (doc opted in) but this task has no entry
//
// Non-goals here: dangling block refs and unparseable sidecars are md run's
// and the structural rules' failure domains — skipped, not double-reported.
package checks

import (
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/run"
)

// sameFileTarget mirrors md run's same-file semantics (run.isSameFile) on
// scan-root-relative paths: empty target, the file's stem, or a path suffix
// of the file's path — [[wiki/doc#^x]] from wiki/doc.md is same-file.
func sameFileTarget(docPath, stem, target string) bool {
	if target == "" || target == stem {
		return true
	}
	target = strings.TrimSuffix(target, ".md")
	pathStem := strings.TrimSuffix(docPath, ".md")
	return pathStem == target || strings.HasSuffix(pathStem, "/"+target)
}

func staleRunRecordCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	fsys, _ := params["__fs"].(fs.FS)
	if fsys == nil {
		return nil
	}
	tasks, err := run.ExtractTasks(doc.Frontmatter)
	if err != nil || len(tasks) == 0 {
		return nil
	}

	stem := strings.TrimSuffix(path.Base(doc.Path), ".md")
	recPath := path.Join(path.Dir(doc.Path), stem+".runs.md")
	data, err := fs.ReadFile(fsys, recPath)
	if err != nil {
		return nil // records are opt-in — no sidecar is a clean state
	}
	recDoc, err := frontmatter.ParseBytes(data)
	if err != nil || recDoc == nil {
		return nil // structural rules own malformed files
	}
	runs, _ := recDoc.Meta["runs"].(map[string]any)

	content := string(doc.RawContent)
	names := make([]string, 0, len(tasks))
	for name := range tasks {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []engine.RawFinding
	for _, name := range names {
		task := tasks[name]
		if task.Ref == "" {
			continue // compositions have no block to hash
		}
		link, err := run.ParseWikilink(task.Ref)
		if err != nil || link.BlockID == "" || !sameFileTarget(doc.Path, stem, link.Target) {
			continue // cross-file and malformed refs are md run's domain
		}
		b, err := run.FindBlock(content, link.BlockID)
		if err != nil {
			continue // dangling ref: md run fails loud, not staleness
		}
		entry, ok := runs[name].(map[string]any)
		if !ok {
			out = append(out, engine.RawFinding{TemplateData: map[string]string{
				"Task":  name,
				"Issue": "never recorded — sidecar has no entry for it; run with record:true",
			}})
			continue
		}
		recorded, _ := entry["block_sha"].(string)
		if recorded != run.BlobSHA(b.Code) {
			out = append(out, engine.RawFinding{TemplateData: map[string]string{
				"Task":  name,
				"Issue": "output predates code — block changed since its recorded run; re-run with record:true",
			}})
		}
	}
	return out
}
