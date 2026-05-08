package partition

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func TestCheckRollup(t *testing.T) {
	// reference_time: 2026-05-07 for deterministic tests
	refTime := "2026-05-07"

	tests := []struct {
		name   string
		doc    *engine.Document
		params map[string]any
		wantN  int
	}{
		{
			name: "3-day-old file in daily folder - pass",
			doc: &engine.Document{
				Path:        "wiki/2026/05/04/page.md",
				Frontmatter: map[string]any{"created": "2026-05-04"},
			},
			params: map[string]any{"reference_time": refTime},
			wantN:  0,
		},
		{
			name: "15-day-old file in daily folder - should roll up",
			doc: &engine.Document{
				Path:        "wiki/2026/04/22/page.md",
				Frontmatter: map[string]any{"created": "2026-04-22"},
			},
			params: map[string]any{"reference_time": refTime},
			wantN:  1,
		},
		{
			name: "60-day-old file in daily folder - should be monthly",
			doc: &engine.Document{
				Path:        "wiki/2026/03/08/page.md",
				Frontmatter: map[string]any{"created": "2026-03-08"},
			},
			params: map[string]any{"reference_time": refTime},
			wantN:  1,
		},
		{
			name: "60-day-old file in monthly folder - pass",
			doc: &engine.Document{
				Path:        "wiki/2026/03/page.md",
				Frontmatter: map[string]any{"created": "2026-03-08"},
			},
			params: map[string]any{"reference_time": refTime},
			wantN:  0,
		},
		{
			name: "15-day-old file in weekly folder - pass",
			doc: &engine.Document{
				Path:        "wiki/2026-W16/page.md",
				Frontmatter: map[string]any{"created": "2026-04-22"},
			},
			params: map[string]any{"reference_time": refTime},
			wantN:  0,
		},
		{
			name: "60-day-old file in weekly folder - should be monthly",
			doc: &engine.Document{
				Path:        "wiki/2026-W10/page.md",
				Frontmatter: map[string]any{"created": "2026-03-08"},
			},
			params: map[string]any{"reference_time": refTime},
			wantN:  1,
		},
		{
			name: "custom thresholds - daily_max_days=3 triggers earlier",
			doc: &engine.Document{
				Path:        "wiki/2026/05/02/page.md",
				Frontmatter: map[string]any{"created": "2026-05-02"},
			},
			params: map[string]any{
				"reference_time": refTime,
				"daily_max_days": 3,
			},
			wantN: 1, // 5 days old, threshold is 3
		},
		{
			name: "custom thresholds - daily_max_days=30 passes",
			doc: &engine.Document{
				Path:        "wiki/2026/04/22/page.md",
				Frontmatter: map[string]any{"created": "2026-04-22"},
			},
			params: map[string]any{
				"reference_time": refTime,
				"daily_max_days": 30,
			},
			wantN: 0, // 15 days old, threshold is 30
		},
		{
			name: "file with no date in path - skip",
			doc: &engine.Document{
				Path:        "wiki/notes/page.md",
				Frontmatter: map[string]any{"created": "2026-03-08"},
			},
			params: map[string]any{"reference_time": refTime},
			wantN:  0,
		},
		{
			name: "no frontmatter date - skip",
			doc: &engine.Document{
				Path:        "wiki/2026/05/04/page.md",
				Frontmatter: map[string]any{},
			},
			params: map[string]any{"reference_time": refTime},
			wantN:  0,
		},
		{
			name: "file at boundary - exactly 7 days in daily - pass",
			doc: &engine.Document{
				Path:        "wiki/2026/04/30/page.md",
				Frontmatter: map[string]any{"created": "2026-04-30"},
			},
			params: map[string]any{"reference_time": refTime},
			wantN:  0, // 7 days = still within threshold
		},
		{
			name: "file at boundary - exactly 8 days in daily - warning",
			doc: &engine.Document{
				Path:        "wiki/2026/04/29/page.md",
				Frontmatter: map[string]any{"created": "2026-04-29"},
			},
			params: map[string]any{"reference_time": refTime},
			wantN:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := CheckRollup(tt.doc, tt.params)
			if len(findings) != tt.wantN {
				t.Errorf("got %d findings, want %d", len(findings), tt.wantN)
				for _, f := range findings {
					t.Logf("  finding: %+v", f)
				}
			}
		})
	}
}
