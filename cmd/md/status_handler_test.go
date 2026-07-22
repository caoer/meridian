package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/caoer/meridian/internal/attest"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
)

// Bare `md status` (no page) must keep answering the legacy watch-daemon query,
// never the drift view — the forwarding-alias guarantee. With no daemon running
// it reports NO_DAEMON, which proves it took the daemon-query path.
func TestStatusBareRoutesToDaemonQuery(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "meridian.yaml")
	h := statusHandler(&config.Config{}, nil, cfgPath)
	resp := h(&cli.Request{Command: "status"}) // Params nil
	if resp.Error == nil || resp.Error.Code != cli.ErrNoDaemon {
		t.Fatalf("bare `md status` must forward to the daemon query (NO_DAEMON), got %+v", resp.Error)
	}
}

// `md watch status` is the same query behind its canonical name.
func TestWatchStatusRoutesToDaemonQuery(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "meridian.yaml")
	h := watchStatusHandler(cfgPath, nil)
	resp := h(&cli.Request{Command: "watch status"})
	if resp.Error == nil || resp.Error.Code != cli.ErrNoDaemon {
		t.Fatalf("md watch status must answer the daemon query (NO_DAEMON), got %+v", resp.Error)
	}
}

// `file` is the run/check spelling; the surface verbs take `page`. A `file` typo
// must point at `page`, never fall through to the daemon query.
func TestStatusRejectsFileKey(t *testing.T) {
	h := statusHandler(&config.Config{}, nil, "cfg.yaml")
	resp := h(&cli.Request{Command: "status", Params: json.RawMessage(`{"file":"x.md"}`)})
	if resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
		t.Fatalf("`file` key must be rejected with a page pointer, got %+v", resp.Error)
	}
}

// A page outside the ledger's sight renders grey/unmanaged through the handler —
// the end-to-end honesty floor.
func TestStatusDriftGreyUnmanaged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "wiki", "plain.md"),
		"---\ntitle: note\ntags: [type/note]\n---\n\n# Plain\n\nprose\n")
	cfg := &config.Config{Scan: config.ScanConfig{Root: root, Skip: []string{".git"}}}

	resp := statusDrift(cfg, nil, json.RawMessage(`{"page":"wiki/plain.md"}`), "wiki/plain.md")
	data, ok := resp.Data.(cli.StatusData)
	if !ok {
		t.Fatalf("want cli.StatusData, got %T (error=%+v)", resp.Data, resp.Error)
	}
	if data.Drift.Color != attest.ColorGrey || data.Drift.State != attest.DriftUnmanaged {
		t.Fatalf("want grey/unmanaged, got %+v", data.Drift)
	}
}

// A frozen copy (HEAD behind origin/<branch>) reads `behind origin/<branch> by
// N` and carries a ref-age line — the origin tip-compare, local refs, no
// network.
func TestComputeFreshnessBehindOrigin(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	writeFile(t, filepath.Join(root, "a.md"), "one\n")
	git("add", "-A")
	git("commit", "-q", "-m", "c1")
	git("commit", "-q", "--allow-empty", "-m", "c2")
	git("update-ref", "refs/remotes/origin/main", "HEAD") // origin/main = c2
	git("reset", "-q", "--hard", "HEAD~1")                 // HEAD = c1, behind by 1

	fr := computeFreshness(root, false)
	if fr == nil {
		t.Fatal("want a freshness line for a git working tree")
	}
	if fr.BehindBy != 1 || fr.Summary != "behind origin/main by 1" {
		t.Fatalf("want `behind origin/main by 1`, got behind_by=%d summary=%q", fr.BehindBy, fr.Summary)
	}
	if fr.RefsFetched == "" {
		t.Fatal("ref-age line must be present (origin refs last fetched ...)")
	}
}

// Non-git directory → no freshness line (never a false "fresh").
func TestComputeFreshnessNonGit(t *testing.T) {
	if fr := computeFreshness(t.TempDir(), false); fr != nil {
		t.Fatalf("want nil freshness outside a git repo, got %+v", fr)
	}
}

// Exec-facts (exit, at, commit) are read verbatim from the page's run record
// sidecar — sourced, never re-derived.
func TestReadRunFactsSourcedFromRecord(t *testing.T) {
	root := t.TempDir()
	// The sidecar for effects/x.md is effects/x.runs.md.
	rec := "---\ntags: [type/run-record]\n" +
		"source: \"[[x]]\"\n" +
		"runs:\n" +
		"  check:\n" +
		"    block: chk\n" +
		"    lang: bash\n" +
		"    exit: 3\n" +
		"    timed_out: false\n" +
		"    duration_ms: 12\n" +
		"    at: \"2026-07-22T00:00:00Z\"\n" +
		"    commit: \"abc1234\"\n" +
		"    block_sha: deadbeef\n" +
		"    truncated: false\n" +
		"---\n\n# check\n\n```\nout\n```\n\n^check\n"
	writeFile(t, filepath.Join(root, "effects", "x.runs.md"), rec)

	rf := readRunFacts(root, "effects/x.md")
	if rf == nil {
		t.Fatal("want run facts from the sidecar")
	}
	got, ok := rf.Tasks["check"]
	if !ok {
		t.Fatalf("want a `check` task fact, got %+v", rf.Tasks)
	}
	if got.Exit != 3 || got.At != "2026-07-22T00:00:00Z" || got.Commit != "abc1234" {
		t.Fatalf("exec-facts not sourced verbatim: %+v", got)
	}
}

// No run record → no fabricated exec-facts.
func TestReadRunFactsAbsent(t *testing.T) {
	if rf := readRunFacts(t.TempDir(), "effects/x.md"); rf != nil {
		t.Fatalf("want nil run facts with no record, got %+v", rf)
	}
}

func writeFile(t *testing.T, abs, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
