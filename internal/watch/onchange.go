package watch

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/hooks"
	"github.com/caoer/meridian/internal/run"
)

// OnChangeResult is one executed md-on-change task — reported in the batch
// JSON line and counted in daemon stats.
type OnChangeResult struct {
	Path   string `json:"path"`
	Task   string `json:"task"`
	Exit   int    `json:"exit"`
	Record string `json:"record,omitempty"`
	Error  string `json:"error,omitempty"`
}

// OnChangeRunner executes a document's declared on-change task, returning
// nil when the document declares none. Injected into the Daemon so unit
// tests can observe dispatch without executing blocks.
type OnChangeRunner func(path string) *OnChangeResult

// RunOnChange is the real runner: if path's frontmatter declares
// `md-on-change: <task>`, execute that task via the md run machinery with
// record:true — the sidecar run record is the inspectable outcome of a run
// nobody watched. Runs are bounded by timeout (a wedged block must not wedge
// the daemon). Returns nil when the doc declares no on-change task; every
// failure mode returns a result, never a panic — the daemon outlives its
// documents.
func RunOnChange(path string, timeout time.Duration) *OnChangeResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // deleted between event and dispatch — a state, not a failure
	}
	doc, err := frontmatter.ParseBytes(data)
	if err != nil || doc == nil {
		return nil
	}
	raw, ok := doc.Meta["md-on-change"]
	if !ok {
		return nil
	}
	task, ok := raw.(string)
	task = strings.TrimSpace(task)
	if !ok || task == "" {
		return &OnChangeResult{Path: path, Task: fmt.Sprint(raw), Exit: -1,
			Error: "md-on-change must be a non-empty task name string"}
	}

	res := &OnChangeResult{Path: path, Task: task}
	results, _, runErr := run.RunTasks(path, []string{task}, nil, timeout, io.Discard, io.Discard, run.Record())
	if len(results) > 0 {
		res.Exit = results[len(results)-1].ExitCode
		if recPath, werr := run.WriteRecord(path, results); werr != nil {
			res.Error = "run record not written: " + werr.Error()
		} else {
			res.Record = recPath
		}
	}
	if runErr != nil {
		res.Error = runErr.Error()
		if len(results) == 0 {
			res.Exit = -1 // resolution failure: nothing executed
		}
	}
	return res
}

// runsSuffix marks sidecar run records. They are outputs, never sources —
// the on-change runner skips them so writing a record never re-triggers the
// document that produced it.
const runsSuffix = ".runs.md"

// dispatchOnChange runs declared on-change tasks for a debounced batch.
// Guards, in order:
//   - only create/modify events on .md files, never .runs.md sidecars
//   - one run per path per batch (a debounced burst is one change)
//   - events stamped before the path's previous run finished are self-caused
//     (the task or its record-write touched the tree while running) — skipped.
//     A task that edits its own file AFTER completing still re-triggers: that
//     hazard is the author's, documented on the usage page.
func (d *Daemon) dispatchOnChange(batch []hooks.Event) []OnChangeResult {
	if d.onChange == nil {
		return nil
	}
	var out []OnChangeResult
	seen := map[string]bool{}
	for _, ev := range batch {
		if ev.Op != "create" && ev.Op != "modify" {
			continue
		}
		if !strings.HasSuffix(ev.Path, ".md") || strings.HasSuffix(ev.Path, runsSuffix) {
			continue
		}
		// Watcher events carry root-relative paths; the runner needs a real
		// one — resolve against the watched root, not the daemon's cwd.
		full := ev.Path
		if d.watcher != nil && !filepath.IsAbs(full) {
			full = filepath.Join(d.watcher.root, full)
		}
		key := filepath.Clean(full)
		if seen[key] {
			continue
		}
		seen[key] = true
		if end, ok := d.lastRunEnd[key]; ok && ev.Time.Before(end) {
			continue
		}
		res := d.onChange(full)
		d.lastRunEnd[key] = time.Now()
		if res != nil {
			out = append(out, *res)
		}
	}
	return out
}
