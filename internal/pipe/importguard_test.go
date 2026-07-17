package pipe

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec" // TEST ONLY: tests may exec; the guarded package may not — that is the point.
	"sort"
	"testing"
)

// importguard_test.go — R7 as a MECHANICAL guard over the link graph.
//
// The daemon is about to link package pipe in-process (Phase D). The R7 exec
// exclusion therefore cannot rest on discipline ("nobody added the import") —
// this test FAILS the moment package pipe's transitive, non-test import set
// gains a path to:
//
//   - github.com/caoer/meridian/internal/run — the exec-capable verbs.
//     Absent from the graph entirely today; must stay absent. No exceptions.
//   - os/exec — two KNOWN residual importers exist, both dead code in the
//     pipe (source-verified against mvdan.cc/sh/v3 v3.13.1; re-verify on any
//     mvdan bump, per the pin note in interp.go):
//       - mvdan.cc/sh/v3/interp links os/exec for its DefaultExecHandler,
//         which can never run: execDispatch (interp.go) is the terminal
//         ExecHandlers middleware and never calls next (pinned at runtime by
//         escape_test.go and TestMdR7_*);
//       - mvdan.cc/sh/v3/internal links os/exec only in TestMainSetup(), a
//         harness helper for mvdan's OWN integration tests; expand calls
//         nothing in that package but ExtendedPatternMatcher.
//     The guard pins those residuals EXACTLY: they are the only packages in
//     the graph permitted to import os/exec. Any new edge — from meridian's
//     own code or a new dependency — fails this test.
//
// Fail-closed: any failure to enumerate the graph (go list error, JSON decode
// error, missing root) is a test FAILURE, never a skip.

const (
	guardRoot     = "github.com/caoer/meridian/internal/pipe"
	forbiddenRun  = "github.com/caoer/meridian/internal/run"
	forbiddenExec = "os/exec"
)

// osExecCarveOut is the pinned set of packages permitted to import os/exec:
// carriers whose exec surface is proven unreachable at runtime. Additions
// require the same adversarial verification interp got (U8/U9a/U9b).
var osExecCarveOut = map[string]bool{
	"mvdan.cc/sh/v3/interp":   true,
	"mvdan.cc/sh/v3/internal": true,
}

func TestR7ImportGuard(t *testing.T) {
	graph := loadDepGraph(t)

	if _, ok := graph[guardRoot]; !ok {
		t.Fatalf("dep graph does not contain %s — enumeration resolved the wrong root; refusing to pass on bad data", guardRoot)
	}

	if _, ok := graph[forbiddenRun]; ok {
		t.Errorf("R7 VIOLATION: %s is link-reachable from package pipe\n  chain: %s",
			forbiddenRun, importChain(graph, guardRoot, forbiddenRun))
	}

	var violators []string
	for pkg, imports := range graph {
		for _, imp := range imports {
			if imp == forbiddenExec && !osExecCarveOut[pkg] {
				violators = append(violators, pkg)
			}
		}
	}
	sort.Strings(violators)
	for _, pkg := range violators {
		t.Errorf("R7 VIOLATION: %s imports os/exec and is not in the pinned carve-out\n  chain: %s -> os/exec",
			pkg, importChain(graph, guardRoot, pkg))
	}
}

// loadDepGraph enumerates package pipe's transitive non-test import graph via
// `go list -deps` (run from the package directory, which is the test's working
// directory). Returns ImportPath -> direct Imports for every package in the
// set, failing the test on any enumeration error.
func loadDepGraph(t *testing.T) map[string][]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-json=ImportPath,Imports", ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps failed (fail-closed): %v\nstderr:\n%s", err, stderr.String())
	}

	graph := make(map[string][]string)
	dec := json.NewDecoder(bytes.NewReader(stdout))
	for {
		var pkg struct {
			ImportPath string
			Imports    []string
		}
		if err := dec.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decoding go list output (fail-closed): %v", err)
		}
		if pkg.ImportPath == "" {
			t.Fatalf("go list emitted a package with no ImportPath (fail-closed)")
		}
		graph[pkg.ImportPath] = pkg.Imports
	}
	if len(graph) == 0 {
		t.Fatalf("go list returned an empty dep set (fail-closed)")
	}
	return graph
}

// importChain returns a shortest root -> target path through the import graph
// for the failure message, so the offending edge is visible without re-running
// go list by hand.
func importChain(graph map[string][]string, root, target string) string {
	prev := map[string]string{root: ""}
	queue := []string{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == target {
			var chain []string
			for n := target; n != ""; n = prev[n] {
				chain = append([]string{n}, chain...)
			}
			out := chain[0]
			for _, n := range chain[1:] {
				out += " -> " + n
			}
			return out
		}
		for _, imp := range graph[cur] {
			if _, seen := prev[imp]; !seen {
				prev[imp] = cur
				queue = append(queue, imp)
			}
		}
	}
	return "(no chain found through the enumerated graph)"
}
