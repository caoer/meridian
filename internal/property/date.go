package property

import (
	"fmt"
	"time"
)

// DateBlock implements TypeBlock for date-typed properties.
type DateBlock struct {
	Format string // Go time format string (default: "2006-01-02")
}

// ParseDate builds a DateBlock from raw YAML.
// Accepts true (default format) or map with format key.
func ParseDate(raw any) (TypeBlock, error) {
	switch v := raw.(type) {
	case bool:
		if !v {
			return nil, fmt.Errorf("date: false not valid")
		}
		return &DateBlock{Format: "2006-01-02"}, nil

	case map[string]any:
		db := &DateBlock{Format: "2006-01-02"}
		if f, ok := v["format"]; ok {
			s, ok := f.(string)
			if !ok {
				return nil, fmt.Errorf("date.format: expected string, got %T", f)
			}
			db.Format = s
		}
		return db, nil

	default:
		return nil, fmt.Errorf("date: expected bool or map, got %T", raw)
	}
}

// Evaluate checks that value parses as a date in the configured format.
func (db *DateBlock) Evaluate(key string, value any, ctx *EvalContext) []Finding {
	// YAML parses unquoted dates (e.g. 2026-04-11) as time.Time.
	// Accept them directly — the value is already a valid date.
	if _, ok := value.(time.Time); ok {
		return nil
	}

	s, ok := value.(string)
	if !ok {
		return []Finding{{
			TemplateData: map[string]string{
				"Key":    key,
				"Value":  fmt.Sprintf("%v", value),
				"Reason": fmt.Sprintf("expected string, got %T", value),
			},
		}}
	}

	_, err := time.Parse(db.Format, s)
	if err != nil {
		return []Finding{{
			TemplateData: map[string]string{
				"Key":    key,
				"Value":  s,
				"Date":   s,
				"Reason": err.Error(),
			},
		}}
	}

	return nil
}
