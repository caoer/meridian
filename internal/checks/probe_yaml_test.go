package checks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/rules"
)

// End-to-end YAML → loader → engine for the probe rule: the shipped rule must
// arrive severity OFF (probes execute code; enabling is a per-wiki decision)
// with its timeout param at the top level (loader contract — params nested
// under params: arrive nil). Execution is then verified through the same
// loaded rule with severity flipped, against a real temp repo.
func TestYAMLPipeline_Probe(t *testing.T) {
	ruleList := loadTestPack(t)
	r := findRule(t, ruleList, "probe")
	if r.Check != "probe" {
		t.Fatalf("Check = %q", r.Check)
	}
	if r.Severity != rules.SeverityOff {
		t.Fatalf("shipped probe rule must be severity off (opt-in), got %v", r.Severity)
	}
	if got, _ := r.Params["timeout"].(string); got != "30s" {
		t.Fatalf("timeout param did not survive the loader: %#v", r.Params["timeout"])
	}

	// Flip severity in-memory (what a wiki's enablement does via profile
	// override) and prove the loaded rule executes and flags a false claim.
	r.Severity = rules.SeverityWarn
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `---
md-probe-claim: "[[claims#^probe-claim]]"
---

` + "```bash" + `
echo "the claim is false" >&2
exit 1
` + "```" + `

^probe-claim
`
	if err := os.WriteFile(filepath.Join(root, "claims.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine()
	e.SetScanRoot(root)
	findings := e.Run(os.DirFS(root), []rules.Rule{r})
	var hit bool
	for _, f := range findings {
		if f.RuleID == "probe" && f.FilePath == "claims.md" &&
			strings.Contains(f.Message, "probe-claim") && strings.Contains(f.Message, "the claim is false") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("probe finding missing; findings: %+v", findings)
	}
}
