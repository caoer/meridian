package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The help surface has one invariant: every command the dispatcher answers is
// cataloged by `md help` with a real one-line description AND answers its own
// `--help`. When the catalog and the dispatch table disagree, the tool's own
// help lies about itself — a listed command with an empty description, or one
// that errors `unknown command` on `--help` (grey rendered as green). These
// tests are that gate: helpSurfaceMismatches is the rule, TestHelpSurfaceParity
// runs it over the real built binary, and the seeded fixture proves it goes red.

// catalogEntry is one row of `md help` list-mode JSON output.
type catalogEntry struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// helpProbe is the outcome of `md <command> --help`.
type helpProbe struct {
	exit int
	out  string
}

// helpSurfaceMismatches returns one message per catalog/dispatch disagreement:
// a cataloged command with an empty description, or one whose `--help` does not
// answer (non-zero exit, empty body, or an unknown-command / malformed-JSON
// error). An empty slice means the catalog and the dispatch table agree. It is
// pure over (catalog, probe) so a seeded fixture can prove the gate fails red
// without building a binary.
func helpSurfaceMismatches(catalog []catalogEntry, probe func(command string) helpProbe) []string {
	var bad []string
	for _, e := range catalog {
		if strings.TrimSpace(e.Description) == "" {
			bad = append(bad, e.Command+": empty description (catalog lists it, the description cell is blank)")
			continue
		}
		p := probe(e.Command)
		switch {
		case p.exit != 0:
			bad = append(bad, fmt.Sprintf("%s: --help exits %d (does not answer)", e.Command, p.exit))
		case strings.TrimSpace(p.out) == "":
			bad = append(bad, e.Command+": --help answered with empty output")
		case strings.Contains(p.out, "unknown command"), strings.Contains(p.out, "malformed JSON"):
			bad = append(bad, e.Command+": --help errors — "+firstLine(p.out))
		}
	}
	return bad
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// buildMD builds the md binary the tests probe — the real dispatch wiring in
// main(), not a router reconstructed in the test (which would drift).
func buildMD(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "md-help-surface")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// mdCatalog runs `md help '{"format":"json"}'` and returns the cataloged
// commands. cwd is a neutral temp dir with no meridian.yaml: help is
// config-free, and the catalog must list the full dispatch table regardless of
// config state.
func mdCatalog(t *testing.T, bin, cwd string) []catalogEntry {
	t.Helper()
	cmd := exec.Command(bin, "help", `{"format":"json"}`)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("md help --format json: %v", err)
	}
	var resp struct {
		Data struct {
			Commands []catalogEntry `json:"commands"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode catalog: %v\n%s", err, out)
	}
	return resp.Data.Commands
}

// TestHelpSurfaceParity is the CI gate: over the real built binary, every
// cataloged command must carry a non-empty description and answer `--help`.
// A router command registered without a help registry entry (or vice versa)
// turns this red.
func TestHelpSurfaceParity(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := buildMD(t)
	cwd := t.TempDir()

	catalog := mdCatalog(t, bin, cwd)
	if len(catalog) == 0 {
		t.Fatal("empty catalog — `md help` listed nothing")
	}

	probe := func(command string) helpProbe {
		args := append(strings.Fields(command), "--help")
		cmd := exec.Command(bin, args...)
		cmd.Dir = cwd
		out, err := cmd.CombinedOutput()
		exit := 0
		if err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("probe %q: %v", command, err)
			}
			exit = ee.ExitCode()
		}
		return helpProbe{exit: exit, out: string(out)}
	}

	if bad := helpSurfaceMismatches(catalog, probe); len(bad) > 0 {
		t.Fatalf("help catalog disagrees with the dispatch table (%d):\n  %s",
			len(bad), strings.Join(bad, "\n  "))
	}
}

// TestHelpAnswersDashHelpAndPositional pins the two discovery fixes end-to-end
// over the real binary: `md help --help` answers with help's own entry (was the
// malformed-JSON error), and `md help <command>` positional sugar answers (the
// natural discovery invocation, was malformed-JSON). Both are the "help before
// error" principle — the tool teaches its own surface without an error hunt.
func TestHelpAnswersDashHelpAndPositional(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := buildMD(t)
	cwd := t.TempDir()

	run := func(args ...string) (int, string) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = cwd
		out, err := cmd.CombinedOutput()
		if err == nil {
			return 0, string(out)
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out)
		}
		t.Fatalf("exec %v: %v", args, err)
		return -1, ""
	}

	for _, args := range [][]string{{"help", "--help"}, {"help", "check"}} {
		exit, out := run(args...)
		if exit != 0 || strings.Contains(out, "malformed JSON") || strings.Contains(out, "unknown command") {
			t.Errorf("md %s: exit %d — want an answer, got %q", strings.Join(args, " "), exit, firstLine(out))
		}
	}
}

// TestHelpSurfaceRootPainNaming pins the root-pain gate: the two existing but
// undiscoverable ≤2-call capabilities must be reachable from `md help` alone —
// the chain-promote scaffold (format:json) and single-entry catalog apply
// (inherit:true + MD_PARAM_PAGE). Named in the catalog, an agent finds them
// without a five-call error hunt.
func TestHelpSurfaceRootPainNaming(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := buildMD(t)
	catalog := mdCatalog(t, bin, t.TempDir())

	var joined strings.Builder
	for _, e := range catalog {
		joined.WriteString(e.Description)
		joined.WriteByte('\n')
	}
	for _, want := range []string{"format:json", "inherit:true", "MD_PARAM_PAGE"} {
		if !strings.Contains(joined.String(), want) {
			t.Errorf("root-pain gate: `md help` catalog must name %q so the ≤2-call loop is reachable from md help alone", want)
		}
	}
}

// TestHelpSurfaceMismatches_SeededFixtureGoesRed proves the gate is not blind:
// a seeded catalog/dispatch mismatch — an empty description, or a listed
// command the dispatcher rejects — must be caught, and a clean catalog must not
// be flagged. Pure (no binary build), so it runs under -short too.
func TestHelpSurfaceMismatches_SeededFixtureGoesRed(t *testing.T) {
	answers := func(string) helpProbe {
		return helpProbe{exit: 0, out: "check: Scan files\n\nUsage:\n  md check <path>"}
	}
	unknown := func(string) helpProbe {
		return helpProbe{exit: 2, out: "error: [INVALID_PARAMS] unknown command: ghost"}
	}

	seeded := []struct {
		name    string
		catalog []catalogEntry
		probe   func(string) helpProbe
	}{
		{
			name:    "empty description",
			catalog: []catalogEntry{{Command: "ghost", Description: "  "}},
			probe:   answers,
		},
		{
			name:    "listed but dispatcher rejects it",
			catalog: []catalogEntry{{Command: "ghost", Description: "a real one-liner"}},
			probe:   unknown,
		},
	}
	for _, tc := range seeded {
		t.Run(tc.name, func(t *testing.T) {
			if bad := helpSurfaceMismatches(tc.catalog, tc.probe); len(bad) == 0 {
				t.Fatalf("seeded %q mismatch was not caught — the CI gate is blind", tc.name)
			}
		})
	}

	// Control: a well-formed entry that answers must never be flagged.
	clean := []catalogEntry{{Command: "check", Description: "Scan files, match rules, evaluate"}}
	if bad := helpSurfaceMismatches(clean, answers); len(bad) != 0 {
		t.Fatalf("clean catalog wrongly flagged: %v", bad)
	}
}
