package partition

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func TestCheckDateConsistency(t *testing.T) {
	tests := []struct {
		name     string
		doc      *engine.Document
		params   map[string]any
		wantN    int // expected number of findings
		wantTmpl string // expected TemplateData key check (empty = skip)
	}{
		{
			name: "daily path matches frontmatter date",
			doc: &engine.Document{
				Path:        "wiki/2026/05/07/page.md",
				Frontmatter: map[string]any{"created": "2026-05-07"},
			},
			params: nil,
			wantN:  0,
		},
		{
			name: "daily path mismatches frontmatter date",
			doc: &engine.Document{
				Path:        "wiki/2026/05/07/page.md",
				Frontmatter: map[string]any{"created": "2026-04-01"},
			},
			params: nil,
			wantN:  1,
			wantTmpl: "Mismatch",
		},
		{
			name: "monthly path matches frontmatter month",
			doc: &engine.Document{
				Path:        "wiki/2026/05/page.md",
				Frontmatter: map[string]any{"created": "2026-05-15"},
			},
			params: nil,
			wantN:  0,
		},
		{
			name: "monthly path mismatches frontmatter month",
			doc: &engine.Document{
				Path:        "wiki/2026/05/page.md",
				Frontmatter: map[string]any{"created": "2026-06-15"},
			},
			params: nil,
			wantN:  1,
		},
		{
			name: "no date pattern in path",
			doc: &engine.Document{
				Path:        "wiki/random/page.md",
				Frontmatter: map[string]any{"created": "2026-05-07"},
			},
			params: nil,
			wantN:  0,
		},
		{
			name: "missing frontmatter date field",
			doc: &engine.Document{
				Path:        "wiki/2026/05/07/page.md",
				Frontmatter: map[string]any{},
			},
			params: nil,
			wantN:  1,
			wantTmpl: "Missing",
		},
		{
			name: "custom date_field param",
			doc: &engine.Document{
				Path:        "wiki/2026/05/07/page.md",
				Frontmatter: map[string]any{"published": "2026-05-07"},
			},
			params: map[string]any{"date_field": "published"},
			wantN:  0,
		},
		{
			name: "custom date_field missing",
			doc: &engine.Document{
				Path:        "wiki/2026/05/07/page.md",
				Frontmatter: map[string]any{"created": "2026-05-07"},
			},
			params: map[string]any{"date_field": "published"},
			wantN:  1,
			wantTmpl: "Missing",
		},
		{
			name: "weekly path matches ISO week",
			doc: &engine.Document{
				Path:        "wiki/2026-W19/page.md",
				Frontmatter: map[string]any{"created": "2026-05-07"},
			},
			params: nil,
			wantN:  0,
		},
		{
			name: "weekly path wrong week",
			doc: &engine.Document{
				Path:        "wiki/2026-W01/page.md",
				Frontmatter: map[string]any{"created": "2026-05-07"},
			},
			params: nil,
			wantN:  1,
		},
		{
			name: "nil frontmatter map",
			doc: &engine.Document{
				Path:        "wiki/2026/05/07/page.md",
				Frontmatter: nil,
			},
			params: nil,
			wantN:  1,
			wantTmpl: "Missing",
		},
		{
			name: "date field is time.Time not string",
			doc: &engine.Document{
				Path:        "wiki/2026/05/07/page.md",
				Frontmatter: map[string]any{"created": "2026-05-07T10:30:00Z"},
			},
			params: nil,
			wantN:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := CheckDateConsistency(tt.doc, tt.params)
			if len(findings) != tt.wantN {
				t.Errorf("got %d findings, want %d", len(findings), tt.wantN)
				for _, f := range findings {
					t.Logf("  finding: %+v", f)
				}
			}
			if tt.wantTmpl != "" && len(findings) > 0 {
				if _, ok := findings[0].TemplateData[tt.wantTmpl]; !ok {
					t.Errorf("expected TemplateData key %q, got %v", tt.wantTmpl, findings[0].TemplateData)
				}
			}
		})
	}
}
