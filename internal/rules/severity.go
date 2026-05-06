package rules

import "fmt"

// Severity controls exit codes and filtering.
type Severity int

const (
	SeverityOff  Severity = iota // loaded but skipped
	SeverityInfo                 // informational — exit 0
	SeverityWarn                 // should fix — exit 0
	SeverityError                // must fix — exit 1
)

var severityNames = map[string]Severity{
	"off":   SeverityOff,
	"info":  SeverityInfo,
	"warn":  SeverityWarn,
	"error": SeverityError,
}

var severityStrings = map[Severity]string{
	SeverityOff:   "off",
	SeverityInfo:  "info",
	SeverityWarn:  "warn",
	SeverityError: "error",
}

// ParseSeverity converts a string to Severity.
func ParseSeverity(s string) (Severity, error) {
	sev, ok := severityNames[s]
	if !ok {
		return 0, fmt.Errorf("unknown severity: %q", s)
	}
	return sev, nil
}

// String returns the canonical string form.
func (s Severity) String() string {
	if name, ok := severityStrings[s]; ok {
		return name
	}
	return fmt.Sprintf("Severity(%d)", int(s))
}
