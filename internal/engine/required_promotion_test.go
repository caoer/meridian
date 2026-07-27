package engine

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/rules"
)

// requiredRule builds a warn rule over wiki/** with the given check and required flag.
func requiredRule(check string, required bool) rules.Rule {
	return rules.Rule{
		ID:       "r-" + check,
		Check:    check,
		Message:  "m",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"wiki/**"}),
		Required: required,
	}
}

// TestRequired_UnregisteredCheck_IsFatal: a required rule whose check is not
// registered promotes CHECK_NOT_REGISTERED from a skipped-rule warning to a
// FatalErr — the tool-failure the CLI maps to exit 2.
func TestRequired_UnregisteredCheck_IsFatal(t *testing.T) {
	fs := makeFS(map[string]string{"wiki/page.md": "---\ntags: [domain/x]\n---\n# Page\n"})

	eng := New()
	results := eng.Run(fs, []rules.Rule{requiredRule("effect-rootless", true)})

	if len(results) != 0 {
		t.Errorf("expected 0 findings from a skipped required rule, got %d", len(results))
	}
	ferr := eng.FatalErr()
	if ferr == nil {
		t.Fatal("expected FatalErr for a required unregistered check, got nil")
	}
	for _, want := range []string{"CHECK_NOT_REGISTERED", "r-effect-rootless", "effect-rootless"} {
		if !strings.Contains(ferr.Error(), want) {
			t.Errorf("FatalErr %q missing %q (must name rule + check)", ferr.Error(), want)
		}
	}
}

// TestOptional_UnregisteredCheck_StaysWarning: the non-required path is
// unchanged — a warning, no FatalErr (a stale binary tolerates optional pack
// growth).
func TestOptional_UnregisteredCheck_StaysWarning(t *testing.T) {
	fs := makeFS(map[string]string{"wiki/page.md": "---\ntags: [domain/x]\n---\n# Page\n"})

	eng := New()
	eng.Run(fs, []rules.Rule{requiredRule("effect-rootless", false)})

	if eng.FatalErr() != nil {
		t.Errorf("optional unregistered check must not set FatalErr, got %v", eng.FatalErr())
	}
	if len(eng.Warnings()) == 0 {
		t.Fatal("expected a CHECK_NOT_REGISTERED warning for the optional path")
	}
}

// TestRequired_RegisteredCheck_Runs: a required rule whose check IS registered
// runs normally — required gates registration, not evaluation.
func TestRequired_RegisteredCheck_Runs(t *testing.T) {
	fs := makeFS(map[string]string{"wiki/page.md": "---\ntags: [domain/x]\n---\n# Page\n"})

	eng := New()
	eng.RegisterCheck("always", func(_ *Document, _ map[string]any) []RawFinding {
		return []RawFinding{{Line: 1}}
	})
	results := eng.Run(fs, []rules.Rule{requiredRule("always", true)})

	if eng.FatalErr() != nil {
		t.Fatalf("registered required check must not be fatal, got %v", eng.FatalErr())
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 finding from the registered check, got %d", len(results))
	}
}

// TestFatalErr_ResetsAcrossRuns: FatalErr is per-run — a clean run after a fatal
// one reports nil (no sticky failure).
func TestFatalErr_ResetsAcrossRuns(t *testing.T) {
	fs := makeFS(map[string]string{"wiki/page.md": "---\ntags: [domain/x]\n---\n# Page\n"})

	eng := New()
	eng.RegisterCheck("always", func(_ *Document, _ map[string]any) []RawFinding { return nil })

	if eng.Run(fs, []rules.Rule{requiredRule("missing", true)}); eng.FatalErr() == nil {
		t.Fatal("expected FatalErr on the first (fatal) run")
	}
	if eng.Run(fs, []rules.Rule{requiredRule("always", true)}); eng.FatalErr() != nil {
		t.Errorf("FatalErr must reset on a clean run, got %v", eng.FatalErr())
	}
}
