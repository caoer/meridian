package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
)

// writeRealiseRepo builds a temp git repo (a bare .git dir is enough for
// GitToplevel) from rel→content and returns the repo root.
func writeRealiseRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func newRealiseRouter() (*cli.Router, *bytes.Buffer) {
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.Handle("realise", realiseHandlerWith(io.Discard))
	r.HandlePositional("realise", pagePositional)
	return r, &out
}

func decodeRealise(t *testing.T, out *bytes.Buffer) (*cli.Response, cli.RealiseData) {
	t.Helper()
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	raw, _ := json.Marshal(resp.Data)
	var data cli.RealiseData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode RealiseData: %v", err)
	}
	return &resp, data
}

// block builds a fenced bash block anchored at ^id.
func block(id, body string) string {
	return "```bash\n" + body + "\n```\n\n^" + id + "\n\n"
}

// checkApplyDoc: a page whose ^check passes iff the marker file `.fixed` exists,
// and whose ^apply creates it. First check drifts; apply fixes; re-check clean.
var checkApplyDoc = "---\n" +
	"md-check: \"[[#^check]]\"\n" +
	"md-apply: \"[[#^apply]]\"\n" +
	"---\n\n" +
	"# Reconcilable\n\n" +
	block("check", "test -f .fixed && exit 0\necho \"drift\" >&2\nexit 1") +
	block("apply", "touch .fixed\necho applied")

// TestRealiseConverged: a page whose check already passes reports converged and
// never fires apply — the CAS level-2 skip. (Quality gate: converged-world.)
func TestRealiseConverged(t *testing.T) {
	doc := "---\nmd-check: \"[[#^check]]\"\nmd-apply: \"[[#^apply]]\"\n---\n\n" +
		block("check", "exit 0") +
		block("apply", "touch APPLY_RAN\necho applied")
	root := writeRealiseRepo(t, map[string]string{"page.md": doc})
	r, out := newRealiseRouter()
	page := filepath.Join(root, "page.md")

	code := r.Run([]string{"realise", `{"page":"` + page + `","format":"json"}`}, nil)
	if code != 0 {
		t.Fatalf("converged must exit 0, got %d: %s", code, out.String())
	}
	_, data := decodeRealise(t, out)
	if data.State != cli.StateConverged {
		t.Fatalf("state = %q, want converged", data.State)
	}
	if !strings.Contains(data.Summary, "nothing ran") {
		t.Errorf("summary = %q", data.Summary)
	}
	if _, err := os.Stat(filepath.Join(root, "APPLY_RAN")); err == nil {
		t.Error("apply fired on a converged page — CAS level-2 skip violated")
	}
	for _, p := range data.Phases {
		if p.Phase == "apply" {
			t.Errorf("apply phase present on a converged page: %+v", p)
		}
	}
}

// TestRealiseDriftedFixedRecordsApply: a drifting page is applied once, re-checks
// clean, and the apply is recorded to the run-record sidecar. (Quality gate:
// cascade — a receipt per fired apply, zero receipt-less applies.)
func TestRealiseDriftedFixedRecordsApply(t *testing.T) {
	root := writeRealiseRepo(t, map[string]string{"page.md": checkApplyDoc})
	r, out := newRealiseRouter()
	page := filepath.Join(root, "page.md")

	code := r.Run([]string{"realise", `{"page":"` + page + `","format":"json"}`}, nil)
	if code != 0 {
		t.Fatalf("drifted-fixed must exit 0, got %d: %s", code, out.String())
	}
	resp, data := decodeRealise(t, out)
	if data.State != cli.StateDriftedFixed {
		t.Fatalf("state = %q, want drifted-fixed\n%s", data.State, out.String())
	}
	// The apply phase must be marked recorded, and RecordPath must be set.
	var apply *cli.RealisePhase
	for i := range data.Phases {
		if data.Phases[i].Phase == "apply" {
			apply = &data.Phases[i]
		}
	}
	if apply == nil || !apply.Recorded || apply.Cap != "mutation" {
		t.Fatalf("apply phase not recorded/mutation: %+v", data.Phases)
	}
	if data.RecordPath == "" {
		t.Fatal("no record_path — a fired apply went unrecorded")
	}
	// The sidecar exists on disk and addresses the apply as ^apply — the receipt.
	rec, err := os.ReadFile(data.RecordPath)
	if err != nil {
		t.Fatalf("run-record sidecar not written: %v", err)
	}
	if !strings.Contains(string(rec), "^apply") {
		t.Errorf("sidecar missing ^apply anchor:\n%s", rec)
	}
	if resp.ExitCode() != 0 {
		t.Errorf("exit = %d, want 0", resp.ExitCode())
	}
	// The apply actually converged the world.
	if _, err := os.Stat(filepath.Join(root, ".fixed")); err != nil {
		t.Errorf("apply did not create the .fixed marker: %v", err)
	}
}

// TestRealiseDryRunZeroCaps: dry-run previews the phases and executes NOTHING —
// no check, no apply, no sidecar. (Quality gate: --dry-run runs with zero caps,
// verified not asserted.)
func TestRealiseDryRunZeroCaps(t *testing.T) {
	doc := "---\nmd-check: \"[[#^check]]\"\nmd-apply: \"[[#^apply]]\"\n---\n\n" +
		block("check", "touch CHECK_RAN\nexit 1") +
		block("apply", "touch APPLY_RAN\necho applied")
	root := writeRealiseRepo(t, map[string]string{"page.md": doc})
	r, out := newRealiseRouter()
	page := filepath.Join(root, "page.md")

	code := r.Run([]string{"realise", `{"page":"` + page + `","dry_run":true,"format":"json"}`}, nil)
	if code != 0 {
		t.Fatalf("dry-run must exit 0, got %d: %s", code, out.String())
	}
	_, data := decodeRealise(t, out)
	if data.State != cli.StatePreview || !data.DryRun {
		t.Fatalf("dry-run state = %q dryRun=%v", data.State, data.DryRun)
	}
	// Zero caps: no block executed — neither sentinel exists.
	for _, sentinel := range []string{"CHECK_RAN", "APPLY_RAN"} {
		if _, err := os.Stat(filepath.Join(root, sentinel)); err == nil {
			t.Errorf("dry-run executed a block (%s exists) — NOT zero caps", sentinel)
		}
	}
	// No sidecar written.
	if _, err := os.Stat(filepath.Join(root, "page.runs.md")); err == nil {
		t.Error("dry-run wrote a run-record sidecar")
	}
	// Preview lists the phases with Ran=false.
	if len(data.Phases) != 2 {
		t.Fatalf("preview phases = %+v (want check + apply)", data.Phases)
	}
	for _, p := range data.Phases {
		if p.Ran {
			t.Errorf("preview phase marked ran: %+v", p)
		}
	}
}

// TestRealiseNonConvergent: check drifts, apply runs and is recorded, but the
// re-check still drifts — the budget of one apply is exhausted. (Quality gate:
// budget-exhaustion → non-convergent.)
func TestRealiseNonConvergent(t *testing.T) {
	doc := "---\nmd-check: \"[[#^check]]\"\nmd-apply: \"[[#^apply]]\"\n---\n\n" +
		block("check", "echo drift >&2\nexit 1") + // never clean
		block("apply", "echo applied") // runs clean but fixes nothing
	root := writeRealiseRepo(t, map[string]string{"page.md": doc})
	r, out := newRealiseRouter()
	page := filepath.Join(root, "page.md")

	code := r.Run([]string{"realise", `{"page":"` + page + `","format":"json"}`}, nil)
	if code != 1 {
		t.Fatalf("non-convergent must exit 1, got %d: %s", code, out.String())
	}
	resp, data := decodeRealise(t, out)
	if data.State != cli.StateNonConvergent {
		t.Fatalf("state = %q, want non-convergent", data.State)
	}
	// The apply still fired and was recorded — no receipt-less apply even when it
	// fails to converge.
	if data.RecordPath == "" {
		t.Error("non-convergent apply went unrecorded")
	}
	if len(resp.Findings) == 0 || resp.Findings[0].Severity != "error" {
		t.Errorf("non-convergent must carry an error finding: %+v", resp.Findings)
	}
}

// TestRealisePendingAgentNoApply: drift with no apply procedure is not
// mechanically repairable — pending-agent.
func TestRealisePendingAgentNoApply(t *testing.T) {
	doc := "---\nmd-check: \"[[#^check]]\"\n---\n\n" +
		block("check", "echo drift >&2\nexit 1")
	root := writeRealiseRepo(t, map[string]string{"page.md": doc})
	r, out := newRealiseRouter()
	page := filepath.Join(root, "page.md")

	code := r.Run([]string{"realise", `{"page":"` + page + `","format":"json"}`}, nil)
	if code != 1 {
		t.Fatalf("pending-agent must exit 1, got %d: %s", code, out.String())
	}
	_, data := decodeRealise(t, out)
	if data.State != cli.StatePendingAgent {
		t.Fatalf("state = %q, want pending-agent", data.State)
	}
	if !strings.Contains(data.Summary, "no `apply`") {
		t.Errorf("summary = %q", data.Summary)
	}
}

// TestRealisePendingAgentApplyFails: an apply that runs but exits non-zero is a
// failed mechanical repair — pending-agent — and it is still recorded.
func TestRealisePendingAgentApplyFails(t *testing.T) {
	doc := "---\nmd-check: \"[[#^check]]\"\nmd-apply: \"[[#^apply]]\"\n---\n\n" +
		block("check", "echo drift >&2\nexit 1") +
		block("apply", "echo 'apply broke' >&2\nexit 3")
	root := writeRealiseRepo(t, map[string]string{"page.md": doc})
	r, out := newRealiseRouter()
	page := filepath.Join(root, "page.md")

	code := r.Run([]string{"realise", `{"page":"` + page + `","format":"json"}`}, nil)
	if code != 1 {
		t.Fatalf("apply-fail pending-agent must exit 1, got %d: %s", code, out.String())
	}
	_, data := decodeRealise(t, out)
	if data.State != cli.StatePendingAgent {
		t.Fatalf("state = %q, want pending-agent", data.State)
	}
	if data.RecordPath == "" {
		t.Error("a failed apply went unrecorded — the record captures the failure too")
	}
}

// TestRealiseInheritsProcedure: a leaf carrying zero machinery inherits its
// check/apply from an ancestor blurb, and realise resolves the loop through the
// same inherit walk md run uses.
func TestRealiseInheritsProcedure(t *testing.T) {
	blurb := "---\nmd-check: \"[[#^check]]\"\nmd-apply: \"[[#^apply]]\"\n---\n\n" +
		block("check", "test -f \"$MD_PARAM_PAGE.ok\" && exit 0\nexit 1") +
		block("apply", "touch \"$MD_PARAM_PAGE.ok\"\necho applied")
	root := writeRealiseRepo(t, map[string]string{
		"effects/skills/SKILLS.md":  blurb,
		"effects/skills/caveman.md": "# Caveman\n", // zero machinery
	})
	r, out := newRealiseRouter()
	leaf := filepath.Join(root, "effects/skills/caveman.md")

	code := r.Run([]string{"realise", `{"page":"` + leaf + `","format":"json"}`}, nil)
	if code != 0 {
		t.Fatalf("inherited realise must exit 0, got %d: %s", code, out.String())
	}
	_, data := decodeRealise(t, out)
	if data.State != cli.StateDriftedFixed {
		t.Fatalf("state = %q, want drifted-fixed via inherit\n%s", data.State, out.String())
	}
	// The check phase resolved from the blurb, not the leaf.
	var check *cli.RealisePhase
	for i := range data.Phases {
		if data.Phases[i].Phase == "check" {
			check = &data.Phases[i]
		}
	}
	if check == nil || check.Source != "effects/skills/SKILLS.md" {
		t.Fatalf("check did not resolve from the blurb: %+v", data.Phases)
	}
}

// TestRealiseFileKeyRejected: the surface takes `page`, never `file` — the shared
// rejection helper points the caller at page (re-run of the card-param-page gate).
func TestRealiseFileKeyRejected(t *testing.T) {
	r, out := newRealiseRouter()
	code := r.Run([]string{"realise", `{"file":"page.md","format":"json"}`}, nil)
	if code != 2 {
		t.Fatalf("file key must exit 2, got %d: %s", code, out.String())
	}
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if resp.Error == nil || !strings.Contains(resp.Error.Message, `use "page"`) {
		t.Errorf("rejection missing the page pointer: %+v", resp.Error)
	}
}

// TestRealisePositionalEqualsJSON: `md realise <page>` == `{"page":"<page>"}`.
func TestRealisePositionalEqualsJSON(t *testing.T) {
	mk := func() string {
		doc := "---\nmd-check: \"[[#^check]]\"\n---\n\n" + block("check", "exit 0")
		root := writeRealiseRepo(t, map[string]string{"page.md": doc})
		return filepath.Join(root, "page.md")
	}
	// positional form
	rp, op := newRealiseRouter()
	rp.SetFormat(cli.FormatJSON)
	if code := rp.Run([]string{"realise", mk()}, nil); code != 0 {
		t.Fatalf("positional exit = %d: %s", code, op.String())
	}
	_, dp := decodeRealise(t, op)
	// json form
	rj, oj := newRealiseRouter()
	if code := rj.Run([]string{"realise", `{"page":"` + mk() + `","format":"json"}`}, nil); code != 0 {
		t.Fatalf("json exit = %d: %s", code, oj.String())
	}
	_, dj := decodeRealise(t, oj)
	if dp.State != dj.State || dp.State != cli.StateConverged {
		t.Errorf("positional/json diverge: %q vs %q", dp.State, dj.State)
	}
}

// TestRealiseNoCheckIsToolFailure: a page with no check procedure cannot be
// reconciled — a tool failure (exit 2), never a page verdict.
func TestRealiseNoCheckIsToolFailure(t *testing.T) {
	root := writeRealiseRepo(t, map[string]string{"page.md": "# Bare\n"})
	r, out := newRealiseRouter()
	page := filepath.Join(root, "page.md")
	code := r.Run([]string{"realise", `{"page":"` + page + `","format":"json"}`}, nil)
	if code != 2 {
		t.Fatalf("no-check page must exit 2, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "no `check` procedure") {
		t.Errorf("error missing the no-check reason: %s", out.String())
	}
}

// TestRealiseMissingPage: page is required.
func TestRealiseMissingPage(t *testing.T) {
	r, out := newRealiseRouter()
	if code := r.Run([]string{"realise", `{"format":"json"}`}, nil); code != 2 {
		t.Fatalf("missing page must exit 2, got %d: %s", code, out.String())
	}
}
