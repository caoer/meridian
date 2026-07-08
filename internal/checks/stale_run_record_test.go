package checks

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/pkg/testkit"
)

// blobSHAHi is the git blob object id of "echo hi\n" — reproducible with
// `printf 'echo hi\n' | git hash-object --stdin`.
const blobSHAHi = "8b2fe5434fec16870a71cd8b272c7fcf6d352536"

const staleSrcDoc = `---
tags: [domain/test]
md-check: "[[doc#^check]]"
md-all: "check"
---

# Doc

` + "```bash" + `
echo hi
` + "```" + `

^check
`

func staleRecordDoc(blockSHA string) string {
	return `---
tags: [type/run-record]
source: "[[doc]]"
runs:
  check:
    block: check
    lang: bash
    exit: 0
    timed_out: false
    duration_ms: 1
    at: "2026-07-08T10:00:00-04:00"
    commit: "abc1234"
    block_sha: ` + blockSHA + `
    truncated: false
---

# check

` + "```" + `
hi
` + "```" + `

^check
`
}

func runStale(t *testing.T, files ...testkit.Entry) []string {
	t.Helper()
	fs := testkit.Wiki(files...)
	rule := testkit.Rule("stale-run-record",
		testkit.Check("stale-run-record"),
		testkit.Severity("warn"),
		testkit.On("**"),
		testkit.MessageTemplate("task {{.Task}}: {{.Issue}}"),
	)
	e := engine.New()
	e.RegisterCheck("stale-run-record", staleRunRecordCheck)
	findings := e.Run(fs, []rules.Rule{rule})
	var msgs []string
	for _, f := range findings {
		msgs = append(msgs, f.FilePath+": "+f.Message)
	}
	return msgs
}

func TestStaleRunRecord_FreshRecordClean(t *testing.T) {
	msgs := runStale(t,
		testkit.F("wiki/doc.md", staleSrcDoc),
		testkit.F("wiki/doc.runs.md", staleRecordDoc(blobSHAHi)),
	)
	if len(msgs) != 0 {
		t.Fatalf("want clean, got %v", msgs)
	}
}

func TestStaleRunRecord_EditedBlockFlagged(t *testing.T) {
	edited := strings.Replace(staleSrcDoc, "echo hi", "echo hi there", 1)
	msgs := runStale(t,
		testkit.F("wiki/doc.md", edited),
		testkit.F("wiki/doc.runs.md", staleRecordDoc(blobSHAHi)),
	)
	if len(msgs) != 1 {
		t.Fatalf("want 1 finding, got %v", msgs)
	}
	if !strings.Contains(msgs[0], "wiki/doc.md") || !strings.Contains(msgs[0], "check") {
		t.Errorf("finding should anchor to the source doc and name the task: %v", msgs[0])
	}
}

func TestStaleRunRecord_NoSidecarClean(t *testing.T) {
	msgs := runStale(t, testkit.F("wiki/doc.md", staleSrcDoc))
	if len(msgs) != 0 {
		t.Fatalf("records are opt-in — no sidecar must be clean, got %v", msgs)
	}
}

func TestStaleRunRecord_TaskMissingFromRecordFlagged(t *testing.T) {
	// Sidecar exists (doc opted in) but records a different task only.
	rec := strings.ReplaceAll(staleRecordDoc(blobSHAHi), "check", "other")
	msgs := runStale(t,
		testkit.F("wiki/doc.md", staleSrcDoc),
		testkit.F("wiki/doc.runs.md", rec),
	)
	if len(msgs) != 1 {
		t.Fatalf("want 1 finding for unrecorded task, got %v", msgs)
	}
	if !strings.Contains(msgs[0], "never recorded") {
		t.Errorf("issue should say never recorded: %v", msgs[0])
	}
}

func TestStaleRunRecord_CompositionAndNonTaskDocsClean(t *testing.T) {
	msgs := runStale(t,
		testkit.F("wiki/plain.md", "---\ntags: [domain/test]\n---\n\nno tasks here\n"),
		// record present but doc has only a composition (md-all) — nothing to hash
		testkit.F("wiki/comp.md", "---\nmd-all: \"x\"\n---\n\nbody\n"),
		testkit.F("wiki/comp.runs.md", staleRecordDoc(blobSHAHi)),
	)
	if len(msgs) != 0 {
		t.Fatalf("want clean, got %v", msgs)
	}
}

func TestStaleRunRecord_UnparseableSidecarClean(t *testing.T) {
	msgs := runStale(t,
		testkit.F("wiki/doc.md", staleSrcDoc),
		testkit.F("wiki/doc.runs.md", ": not yaml\n"),
	)
	if len(msgs) != 0 {
		t.Fatalf("unparseable sidecar is a structural problem, not staleness: %v", msgs)
	}
}

func TestStaleRunRecord_DanglingRefSkipped(t *testing.T) {
	doc := strings.Replace(staleSrcDoc, "^check", "^renamed", 1)
	msgs := runStale(t,
		testkit.F("wiki/doc.md", doc),
		testkit.F("wiki/doc.runs.md", staleRecordDoc(blobSHAHi)),
	)
	// Broken block ref is md run's failure domain, not staleness.
	if len(msgs) != 0 {
		t.Fatalf("dangling ref should be skipped, got %v", msgs)
	}
}
