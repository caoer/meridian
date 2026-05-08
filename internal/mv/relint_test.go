package mv

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/vfs"
)

// stubCheck returns a finding for every document it sees.
func stubCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	return []engine.RawFinding{{
		Line:         1,
		TemplateData: map[string]string{"File": doc.Path},
	}}
}

func TestRelint_FindingsAtNewLocation(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/infra/page.md", "---\ntags: [domain/infra]\n---\nBody")

	eng := engine.New()
	eng.RegisterCheck("stub", stubCheck)

	rl := []rules.Rule{{
		ID:       "r1",
		Check:    "stub",
		Message:  "found {{.File}}",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"wiki/infra/**"}),
	}}

	findings := Relint(m, eng, rl, []string{"wiki/infra/page.md"})
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].FilePath != "wiki/infra/page.md" {
		t.Errorf("path = %q", findings[0].FilePath)
	}
}

func TestRelint_StillReported(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/page.md", "---\n---\nBody")

	eng := engine.New()
	eng.RegisterCheck("stub", stubCheck)

	rl := []rules.Rule{{
		ID:       "r1",
		Check:    "stub",
		Message:  "found",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"wiki/**"}),
	}}

	findings := Relint(m, eng, rl, []string{"wiki/page.md"})
	if len(findings) != 1 {
		t.Fatalf("want 1, got %d", len(findings))
	}
}

func TestRelint_NoMatchingRules(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("archive/page.md", "---\n---\nBody")

	eng := engine.New()
	eng.RegisterCheck("stub", stubCheck)

	rl := []rules.Rule{{
		ID:       "r1",
		Check:    "stub",
		Message:  "found",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"wiki/**"}),
	}}

	findings := Relint(m, eng, rl, []string{"archive/page.md"})
	if len(findings) != 0 {
		t.Fatalf("want 0, got %d", len(findings))
	}
}

func TestRelint_OnlyMovedFiles(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/moved.md", "---\n---\nBody")
	m.AddFile("wiki/other.md", "---\n---\nBody")

	eng := engine.New()
	eng.RegisterCheck("stub", stubCheck)

	rl := []rules.Rule{{
		ID:       "r1",
		Check:    "stub",
		Message:  "found",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"wiki/**"}),
	}}

	// Only wiki/moved.md was moved
	findings := Relint(m, eng, rl, []string{"wiki/moved.md"})
	if len(findings) != 1 {
		t.Fatalf("want 1, got %d", len(findings))
	}
	if findings[0].FilePath != "wiki/moved.md" {
		t.Errorf("path = %q, want wiki/moved.md", findings[0].FilePath)
	}
}
