package checks_test

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/pkg/testkit"
)

// End-to-end YAML → loader → engine → finding: guards the loader blind spot
// documented in the architecture page (VFS-only tests never exercised rule
// YAML, letting nil-params bugs ship).
func TestYAMLPipeline_StaleRunRecord(t *testing.T) {
	ruleList := loadTestPack(t)
	r := findRule(t, ruleList, "stale-run-record")
	if r.Check != "stale-run-record" {
		t.Fatalf("Check = %q", r.Check)
	}

	fs := testkit.Wiki(
		testkit.F("wiki/doc.md", `---
md-check: "[[doc#^check]]"
---

`+"```bash"+`
echo changed since the record
`+"```"+`

^check
`),
		testkit.F("wiki/doc.runs.md", `---
tags: [type/run-record]
runs:
  check:
    block: check
    block_sha: 0000000000000000000000000000000000000000
---

# check

`+"```"+`
old output
`+"```"+`

^check
`),
	)
	findings := newTestEngine().Run(fs, ruleList)
	var hit bool
	for _, f := range findings {
		if f.RuleID == "stale-run-record" && f.FilePath == "wiki/doc.md" &&
			strings.Contains(f.Message, "task check") && strings.Contains(f.Message, "predates") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("stale-run-record finding missing; findings: %+v", findings)
	}
}
