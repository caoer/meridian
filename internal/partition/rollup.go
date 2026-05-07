package partition

import (
	"fmt"
	"time"

	"github.com/caoer/meridian/internal/engine"
)

const (
	defaultDailyMaxDays  = 7
	defaultWeeklyMaxDays = 30
)

// CheckRollup surfaces warnings when files are in wrong granularity
// folder based on age. Daily folders should roll up to weekly/monthly
// after daily_max_days; weekly should roll up to monthly after weekly_max_days.
func CheckRollup(doc *engine.Document, params map[string]any) []engine.RawFinding {
	dateField := "created"
	if p, ok := params["date_field"]; ok {
		if s, ok := p.(string); ok {
			dateField = s
		}
	}

	dailyMax := defaultDailyMaxDays
	if p, ok := params["daily_max_days"]; ok {
		dailyMax = toInt(p, defaultDailyMaxDays)
	}

	weeklyMax := defaultWeeklyMaxDays
	if p, ok := params["weekly_max_days"]; ok {
		weeklyMax = toInt(p, defaultWeeklyMaxDays)
	}

	now := time.Now()
	if p, ok := params["reference_time"]; ok {
		if s, ok := p.(string); ok {
			if t, ok := parseDate(s); ok {
				now = t
			}
		}
	}

	// Detect path granularity.
	_, _, _, _, kind := extractPathDate(doc.Path)
	if kind == "" {
		return nil // not date-partitioned
	}

	// Get frontmatter date.
	fmDate, ok := getFrontmatterDate(doc.Frontmatter, dateField)
	if !ok {
		return nil // no date → can't determine age
	}

	ageDays := int(now.Sub(fmDate).Hours() / 24)

	switch kind {
	case "daily":
		if ageDays > dailyMax {
			suggested := "weekly or monthly"
			if ageDays > weeklyMax {
				suggested = "monthly"
			}
			return []engine.RawFinding{{
				Line: 0,
				TemplateData: map[string]string{
					"Age":       fmt.Sprintf("%d days", ageDays),
					"Current":   "daily",
					"Suggested": suggested,
				},
			}}
		}
	case "weekly":
		if ageDays > weeklyMax {
			return []engine.RawFinding{{
				Line: 0,
				TemplateData: map[string]string{
					"Age":       fmt.Sprintf("%d days", ageDays),
					"Current":   "weekly",
					"Suggested": "monthly",
				},
			}}
		}
	}
	// monthly folders never need rollup

	return nil
}

// toInt converts any numeric-ish value to int.
func toInt(v any, fallback int) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	default:
		return fallback
	}
}
