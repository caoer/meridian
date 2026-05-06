package cli

import (
	"fmt"
	"io"
	"strings"
)

// RenderText writes a human-readable representation of the response.
func RenderText(w io.Writer, resp *Response) {
	// Error: single line
	if resp.Error != nil {
		fmt.Fprintf(w, "error: [%s] %s\n", resp.Error.Code, resp.Error.Message)
		if resp.Error.Hint != "" {
			fmt.Fprintf(w, "  hint: %s\n", resp.Error.Hint)
		}
		return
	}

	// Findings (check command)
	if resp.Findings != nil {
		errCount := 0
		warnCount := 0
		infoCount := 0

		for _, f := range resp.Findings {
			level := strings.ToUpper(f.Severity)
			loc := f.FilePath
			if f.Line > 0 {
				loc = fmt.Sprintf("%s:%d", f.FilePath, f.Line)
			}
			fmt.Fprintf(w, "%-5s %s  [%s] %s\n", level, loc, f.RuleID, f.Message)

			switch f.Severity {
			case "error":
				errCount++
			case "warn":
				warnCount++
			case "info":
				infoCount++
			}
		}

		// Summary line
		if resp.Stats != nil {
			fmt.Fprintln(w)
			parts := []string{}
			if resp.Stats.RulesApplied > 0 {
				parts = append(parts, fmt.Sprintf("%d rules", resp.Stats.RulesApplied))
			}
			parts = append(parts, fmt.Sprintf("%d errors", errCount))
			parts = append(parts, fmt.Sprintf("%d warnings", warnCount))
			if resp.Stats.DurationMs > 0 {
				parts = append(parts, fmt.Sprintf("%dms", resp.Stats.DurationMs))
			}
			fmt.Fprintln(w, strings.Join(parts, ", "))
		} else if len(resp.Findings) == 0 {
			fmt.Fprintln(w, "clean")
		}

		// Warnings (engine warnings, not findings)
		for _, w2 := range resp.Warnings {
			fmt.Fprintf(w, "NOTE  [%s] %s\n", w2.Code, w2.Message)
		}
		return
	}

	// Data payloads (rules ls, debug, help, version) — structured text
	if resp.Data != nil {
		formatData(w, resp.Data)
		return
	}

	fmt.Fprintln(w, "ok")
}

// formatData renders command-specific data payloads as text.
func formatData(w io.Writer, data any) {
	switch d := data.(type) {
	case map[string]string:
		// version
		for k, v := range d {
			fmt.Fprintf(w, "%s: %s\n", k, v)
		}

	case map[string]any:
		// help, debug, generic
		formatMap(w, d, "")

	case RulesLsData:
		// rules ls
		if len(d.Rules) == 0 {
			fmt.Fprintln(w, "no rules loaded")
			return
		}
		for _, r := range d.Rules {
			onStr := formatOn(r.On)
			fmt.Fprintf(w, "  %-30s %-15s %-6s  on: %s\n", r.ID, r.Check, r.Severity, onStr)
		}
		fmt.Fprintf(w, "\n%d rules\n", d.Total)

	case DebugData:
		// debug
		r := d.Rule
		registered := "yes"
		if !r.CheckRegistered {
			registered = "NO"
		}
		fmt.Fprintf(w, "rule:       %s\n", r.ID)
		fmt.Fprintf(w, "check:      %s (registered: %s)\n", r.Check, registered)
		fmt.Fprintf(w, "severity:   %s\n", r.Severity)
		fmt.Fprintf(w, "on:         %s\n", formatOn(r.On))
		if len(r.Params) > 0 {
			fmt.Fprintln(w, "params:")
			for k, v := range r.Params {
				fmt.Fprintf(w, "  %s: %v\n", k, v)
			}
		}

	case HelpListData:
		for _, c := range d.Commands {
			fmt.Fprintf(w, "  %-15s %s\n", c.Command, c.Description)
		}

	case HelpCommandData:
		fmt.Fprintf(w, "%s: %s\n", d.Command, d.Description)
		if len(d.Params) > 0 {
			fmt.Fprintln(w, "\nParameters:")
			for name, p := range d.Params {
				req := ""
				if p.Required {
					req = " (required)"
				}
				fmt.Fprintf(w, "  %-12s %s%s\n", name, p.Type, req)
			}
		}
		if len(d.ExitCodes) > 0 {
			fmt.Fprintln(w, "\nExit codes:")
			for code, desc := range d.ExitCodes {
				fmt.Fprintf(w, "  %s  %s\n", code, desc)
			}
		}

	case HelpSearchData:
		if len(d.Results) == 0 {
			fmt.Fprintln(w, "no results")
			return
		}
		for _, r := range d.Results {
			fmt.Fprintf(w, "  [%s] %-30s %s\n", r.Type, r.ID, r.Description)
		}

	default:
		// Fallback — just print
		fmt.Fprintf(w, "%v\n", data)
	}
}

func formatMap(w io.Writer, m map[string]any, indent string) {
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			fmt.Fprintf(w, "%s%s:\n", indent, k)
			formatMap(w, sub, indent+"  ")
		} else {
			fmt.Fprintf(w, "%s%s: %v\n", indent, k, v)
		}
	}
}

func formatOn(on any) string {
	switch v := on.(type) {
	case string:
		return v
	case []string:
		return strings.Join(v, ", ")
	default:
		return fmt.Sprintf("%v", on)
	}
}
