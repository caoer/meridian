// probe: executable claims. A doc declares probes as ordinary md-run tasks
// under a naming convention — `md-probe-<name>` frontmatter keys — and this
// check executes them during `md check`, turning prose claims ("the daemon
// listens on :8787") into empirically falsifiable ones. A probe that exits
// non-zero, times out, or cannot resolve is a finding on the declaring doc,
// never a crash.
//
// Safety rails, in the rule YAML and by construction:
//
//   - opt-in scope: the shipped rule is severity off; enabling means scoping
//     `on:` deliberately — probes never run by default
//   - per-probe wall-clock timeout (`timeout` param, default 30s), reusing
//     md run's process-group kill semantics (exit 124)
//   - sequential execution via run.RunTasks — one probe at a time, each an
//     isolated invocation so one failure never hides another's verdict
//
// Execution needs real paths (an interpreter cannot cd into a MemFS), so the
// check runs only when the engine carries a scan root (__scan_root); pure-VFS
// runs skip. Everything else — resolution, contracts, cross-file refs, cwd =
// defining file's toplevel — is md run's, by delegation, not reimplementation.
package checks

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/run"
)

// probePrefix is the task-name prefix (after md-) marking a task as a probe.
const probePrefix = "probe-"

// probeDefaultTimeout bounds a probe with no explicit timeout param — a check
// run must never hang on a wedged claim.
const probeDefaultTimeout = 30 * time.Second

func probeCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	root, _ := params["__scan_root"].(string)
	if root == "" {
		return nil // pure-VFS run: no real paths to execute against
	}
	tasks, err := run.ExtractTasks(doc.Frontmatter)
	if err != nil || len(tasks) == 0 {
		return nil // malformed task tables are md run's / structural rules' domain
	}

	var probes []string
	for name := range tasks {
		if strings.HasPrefix(name, probePrefix) && tasks[name].Ref != "" {
			probes = append(probes, name)
		}
	}
	if len(probes) == 0 {
		return nil
	}
	sort.Strings(probes)

	timeout := probeDefaultTimeout
	if s, ok := params["timeout"].(string); ok {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			timeout = d
		}
	}

	mdPath := filepath.Join(root, filepath.FromSlash(doc.Path))
	var out []engine.RawFinding
	for _, name := range probes {
		// One RunTasks call per probe: isolated resolution and execution, so a
		// dangling ref or failure in one probe never aborts the others.
		results, _, err := run.RunTasks(mdPath, []string{name}, nil, timeout, io.Discard, io.Discard)
		if err != nil {
			out = append(out, engine.RawFinding{TemplateData: map[string]string{
				"Probe":  name,
				"Detail": "not executable — " + err.Error(),
			}})
			continue
		}
		r := results[len(results)-1]
		if r.ExitCode == 0 {
			continue
		}
		detail := fmt.Sprintf("exit %d", r.ExitCode)
		if r.TimedOut {
			detail = fmt.Sprintf("timed out after %s (exit %d)", timeout, r.ExitCode)
		}
		if r.Stderr != "" {
			detail += " — " + r.Stderr
		}
		out = append(out, engine.RawFinding{TemplateData: map[string]string{
			"Probe":  name,
			"Detail": detail,
		}})
	}
	return out
}
