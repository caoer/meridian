package partition

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/engine"
)

// Path patterns for date extraction.
var (
	// YYYY/MM/DD/ — daily partition
	dailyRe = regexp.MustCompile(`(?:^|/)(\d{4})/(\d{2})/(\d{2})/`)
	// YYYY/MM/ — monthly partition (but NOT daily — daily matched first)
	monthlyRe = regexp.MustCompile(`(?:^|/)(\d{4})/(\d{2})/[^/]*$`)
	// YYYY-WNN/ — ISO week partition
	weeklyRe = regexp.MustCompile(`(?:^|/)(\d{4})-W(\d{2})/`)
)

// CheckDateConsistency verifies that a document's folder-path date
// matches its frontmatter date field.
// Path patterns detected: YYYY/MM/DD/, YYYY/MM/, YYYY-WNN/ (ISO week).
func CheckDateConsistency(doc *engine.Document, params map[string]any) []engine.RawFinding {
	dateField := "created"
	if p, ok := params["date_field"]; ok {
		if s, ok := p.(string); ok {
			dateField = s
		}
	}

	// Detect path pattern — daily first, then weekly, then monthly.
	pathYear, pathMonth, pathDay, pathWeek, kind := extractPathDate(doc.Path)
	if kind == "" {
		return nil // not a date-partitioned path
	}

	// Get frontmatter date.
	fmDate, ok := getFrontmatterDate(doc.Frontmatter, dateField)
	if !ok {
		return []engine.RawFinding{{
			Line: 1,
			TemplateData: map[string]string{
				"Missing": dateField,
				"Reason":  fmt.Sprintf("frontmatter field %q not found", dateField),
			},
		}}
	}

	// Compare based on path granularity.
	switch kind {
	case "daily":
		fmY, fmM, fmD := fmDate.Date()
		if pathYear != fmY || int(fmM) != pathMonth || fmD != pathDay {
			return []engine.RawFinding{{
				Line: 1,
				TemplateData: map[string]string{
					"Mismatch": "date",
					"PathDate": fmt.Sprintf("%04d-%02d-%02d", pathYear, pathMonth, pathDay),
					"FmDate":   fmDate.Format("2006-01-02"),
				},
			}}
		}
	case "monthly":
		fmY, fmM, _ := fmDate.Date()
		if pathYear != fmY || int(fmM) != pathMonth {
			return []engine.RawFinding{{
				Line: 1,
				TemplateData: map[string]string{
					"Mismatch": "month",
					"PathDate": fmt.Sprintf("%04d-%02d", pathYear, pathMonth),
					"FmDate":   fmt.Sprintf("%04d-%02d", fmY, int(fmM)),
				},
			}}
		}
	case "weekly":
		fmY, fmW := fmDate.ISOWeek()
		if pathYear != fmY || pathWeek != fmW {
			return []engine.RawFinding{{
				Line: 1,
				TemplateData: map[string]string{
					"Mismatch": "week",
					"PathDate": fmt.Sprintf("%04d-W%02d", pathYear, pathWeek),
					"FmDate":   fmt.Sprintf("%04d-W%02d", fmY, fmW),
				},
			}}
		}
	}

	return nil
}

// extractPathDate returns year, month, day, week, kind from path.
// kind is "daily", "monthly", "weekly", or "" if no pattern found.
func extractPathDate(path string) (year, month, day, week int, kind string) {
	if m := dailyRe.FindStringSubmatch(path); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if mo < 1 || mo > 12 || d < 1 || d > 31 {
			return 0, 0, 0, 0, ""
		}
		// Validate the date is real (rejects Feb 30, Apr 31, etc.)
		t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
		if t.Day() != d {
			return 0, 0, 0, 0, ""
		}
		return y, mo, d, 0, "daily"
	}
	if m := weeklyRe.FindStringSubmatch(path); m != nil {
		y, _ := strconv.Atoi(m[1])
		w, _ := strconv.Atoi(m[2])
		if w < 1 || w > 53 {
			return 0, 0, 0, 0, ""
		}
		return y, 0, 0, w, "weekly"
	}
	if m := monthlyRe.FindStringSubmatch(path); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		if mo < 1 || mo > 12 {
			return 0, 0, 0, 0, ""
		}
		return y, mo, 0, 0, "monthly"
	}
	return 0, 0, 0, 0, ""
}

// getFrontmatterDate extracts a time.Time from frontmatter field.
// Supports string dates (YYYY-MM-DD, RFC3339) and time.Time values.
func getFrontmatterDate(fm map[string]any, field string) (time.Time, bool) {
	if fm == nil {
		return time.Time{}, false
	}
	v, ok := fm[field]
	if !ok {
		return time.Time{}, false
	}

	switch val := v.(type) {
	case time.Time:
		return val, true
	case string:
		return parseDate(val)
	default:
		return time.Time{}, false
	}
}

// parseDate tries common date formats.
func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		"2006-01-02",
		time.RFC3339,
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
