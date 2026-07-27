package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caoer/meridian/internal/attest"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/types"
)

// TestProcedureHashWriterVerifierAgree is the Seam-A cross-derivation gate. The
// WRITER (`md attest`, internal/attest → resolve.ProcedureHash via its
// FactSource) records a receipt `procedure-hash`; the VERIFIER (chain-fresh
// cond-4, internal/checks → resolve.ProcedureHash via the engine.Facts table)
// checks it. Post-unification both route through the ONE resolve.ProcedureHash,
// so a fresh receipt reproduces (MATCH — no `procedure` finding), and editing
// the ^check block moves the resolved procedure and fires the `procedure` cause
// (MISMATCH). The blindspot that let the fork ship — cond-4 was only ever tested
// against its own derivation — is closed by pitting the two independent code
// paths against each other. No hash constant is captured from either side; the
// assertion is the design's match/mismatch behavior.
func TestProcedureHashWriterVerifierAgree(t *testing.T) {
	if testing.Short() {
		t.Skip("scans + attests a real corpus")
	}
	effectRel := "effects/skills/x.md"
	page := func(checkBody string) string {
		return "---\n" +
			"name: x\n" +
			"repo: cc-continuity\n" +
			"location: skills/x/\n" +
			"inputs: '[[#^inputs]]'\n" +
			"receipt: null\n" +
			"tags: [type/effect]\n" +
			"---\n\n# X\n\n## Chain\n\n" +
			"```yaml\n- ref: '[[dep#Sec]]'\n  hash: null\n```\n^inputs\n\n" +
			"## Procedure\n\n" +
			"```bash\n" + checkBody + "\n```\n^check\n\n" +
			"```bash\necho apply-v1\n```\n^apply\n"
	}
	dep := "---\ntags: [type/note]\n---\n\n# Dep\n\n## Sec\n\nstable external content\n"

	root := t.TempDir()
	mustWrite := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(effectRel, page("echo check-v1"))
	mustWrite("wiki/dep.md", dep)

	reposRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(reposRoot, "cc-continuity"), 0o755); err != nil {
		t.Fatal(err)
	}

	// WRITER — md attest with fake git/check seams but the REAL procedure-hash
	// derivation (ProcHash left at its resolve.ProcedureHash default).
	h := attestHandlerWith(os.DirFS(root), root, engine.ScanOptions{}, func(e *attest.Engine) {
		e.Git = &fakeAttestGit{objs: map[string]string{
			"origin/main":           attestTip + " commit 250",
			attestTip + ":skills/x": attestSum + " tree 0",
		}}
		e.ReposRoot = reposRoot
		e.Branch = func(slug string) (string, bool) { return "main", slug == "cc-continuity" }
		e.RunCheck = func(string) attest.CheckResult { return attest.CheckResult{} }
		e.Now = func() time.Time { return time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC) }
	})
	if _, code := runAttest(t, h, `{"page":"`+effectRel+`"}`); code != 0 {
		t.Fatalf("writer attest failed, exit %d", code)
	}
	written, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(effectRel)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "procedure-hash:") {
		t.Fatalf("writer did not record a procedure-hash:\n%s", written)
	}

	// VERIFIER — chain-fresh cond-4, fresh pack engine each run.
	procFindings := func() []types.Finding {
		eng, loaded := newPackEngine(t)
		var out []types.Finding
		for _, f := range warnsByRule(eng.Run(os.DirFS(root), loaded), "effect-chain-fresh") {
			if strings.Contains(f.Message, "[procedure]") {
				out = append(out, f)
			}
		}
		return out
	}

	// MATCH — the writer's hash reproduces under the verifier: no procedure cause.
	if got := procFindings(); len(got) != 0 {
		t.Fatalf("MATCH failed: writer receipt did not reproduce under the verifier — %v", got)
	}

	// MISMATCH — a ^check edit moves the resolved procedure; recorded stays.
	mutated := strings.Replace(string(written), "echo check-v1", "echo check-v2", 1)
	if mutated == string(written) {
		t.Fatal("failed to mutate the ^check block")
	}
	mustWrite(effectRel, mutated)
	if got := procFindings(); len(got) != 1 {
		t.Fatalf("MISMATCH failed: a ^check edit must fire exactly one `procedure` finding, got %d: %v", len(got), got)
	}
}
