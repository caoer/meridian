package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
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

	// Data payloads (rules ls, debug, help, version) — structured text.
	// Warnings go to stderr: a strict-decode flag invisible in default
	// output would be silent normalization from the caller's seat; stdout
	// stays pure data (the md read precedent).
	if resp.Data != nil {
		formatData(w, resp.Data)
		for _, w2 := range resp.Warnings {
			fmt.Fprintf(os.Stderr, "WARN  [%s] %s\n", w2.Code, w2.Message)
		}
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

	case DomainsTreeData:
		if len(d.Domains) == 0 {
			fmt.Fprintln(w, "no domains")
			return
		}
		// Sort prefixes for deterministic output
		prefixes := make([]string, 0, len(d.Domains))
		for p := range d.Domains {
			prefixes = append(prefixes, p)
		}
		sort.Strings(prefixes)
		for _, p := range prefixes {
			dom := d.Domains[p]
			fmt.Fprintf(w, "%s/\n", p)
			for _, v := range dom.Values {
				fmt.Fprintf(w, "  %-20s %d\n", v, dom.Counts[v])
			}
		}
		fmt.Fprintf(w, "\n%d prefixes, %d pages\n", d.TotalPrefixes, d.TotalPages)

	case DomainsShowData:
		fmt.Fprintf(w, "%s/\n", d.Prefix)
		for _, v := range d.Values {
			fmt.Fprintf(w, "  %-20s %d\n", v.Name, v.Count)
		}

	case HelpSearchData:
		if len(d.Results) == 0 {
			fmt.Fprintln(w, "no results")
			return
		}
		for _, r := range d.Results {
			fmt.Fprintf(w, "  [%s] %-30s %s\n", r.Type, r.ID, r.Description)
		}

	case FixData:
		for _, f := range d.Fixed {
			fmt.Fprintf(w, "FIX   %-30s [%s] %s\n", f.FilePath, f.RuleID, f.Action)
		}
		for _, s := range d.Unfixable {
			fmt.Fprintf(w, "SKIP  %-30s [%s] %s\n", s.FilePath, s.RuleID, s.Reason)
		}
		fmt.Fprintf(w, "\n%d fixed, %d unfixable\n", d.FixedCount, d.UnfixableCount)

	case DebtData:
		if len(d.Entries) == 0 {
			fmt.Fprintln(w, "no incorporation debt")
			return
		}
		for _, e := range d.Entries {
			fmt.Fprintf(w, "  %-10s  %-22s  %s\n", e.Created, e.Where, e.Source)
		}
		fmt.Fprintf(w, "\n%d sources\n", d.Total)

	case LLMWikiCheckData:
		fmt.Fprintf(w, "wiki:       %s (source: %s)\n", d.WikiPath, d.WikiSource)
		fmt.Fprintf(w, "repos-root: %s\n", d.ReposRoot)
		fmt.Fprintf(w, "repos:      %d cataloged · %d present (%d via symlink) · %d absent\n",
			d.Total, d.Present, d.Symlinked, d.Absent)
		if len(d.Drifted) > 0 {
			fmt.Fprintf(w, "catalog-stale (HEAD != sources/git catalog commit): %d — %s\n", len(d.Drifted), strings.Join(d.Drifted, ", "))
		}
		for _, p := range d.Problems {
			fmt.Fprintf(w, "PROBLEM %-24s %-16s %s\n", p.Slug, p.State, p.Detail)
		}

	case RulesCheckData:
		if len(d.Conflicts) > 0 {
			fmt.Fprintln(w, "Conflicts:")
			for _, c := range d.Conflicts {
				fmt.Fprintf(w, "  %-5s %s vs %s: %s\n", strings.ToUpper(c.Severity), c.RuleA, c.RuleB, c.Detail)
			}
		}
		if len(d.OverlapsWithoutConflict) > 0 {
			if len(d.Conflicts) > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, "Overlaps (no conflict):")
			for _, o := range d.OverlapsWithoutConflict {
				fmt.Fprintf(w, "  %s vs %s: %s\n", o.RuleA, o.RuleB, o.Reason)
			}
		}
		if len(d.Conflicts) == 0 && len(d.OverlapsWithoutConflict) == 0 {
			fmt.Fprintln(w, "no conflicts or overlaps detected")
		}

	case ReadData:
		// stdout carries pure content (automation reads it); base/match
		// metadata goes to stderr from the handler. Separators only on
		// multi-match.
		if len(d.Matches) == 1 {
			io.WriteString(w, d.Matches[0].Content)
			return
		}
		for i, m := range d.Matches {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "--- %s ---\n", m.Path)
			io.WriteString(w, m.Content)
		}

	case RunData:
		// Task rows follow the silence-is-ok law: success prints nothing
		// (harness fenced-! blocks merge stderr into the capture, so even
		// stderr ok-rows pollute the injected render); failure prints loud
		// on stderr. stdout stays the byte-for-byte block-output channel.
		// JSON format keeps the full task list, both statuses.
		for _, task := range d.Tasks {
			if task.ExitCode == 0 {
				continue
			}
			fmt.Fprintf(os.Stderr, "TASK  %-15s ^%-15s %-7s FAILED (exit %d)\n", task.Name, task.BlockID, task.Lang, task.ExitCode)
		}

	case SchemaData:
		// Human-readable: sorted keys, nested maps indented.
		keys := make([]string, 0, len(d.Schema))
		for k := range d.Schema {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := d.Schema[k]
			if sub, ok := v.(map[string]any); ok {
				fmt.Fprintf(w, "%s:\n", k)
				formatMap(w, sub, "  ")
			} else {
				fmt.Fprintf(w, "%s: %v\n", k, v)
			}
		}

	case RunListData:
		for _, task := range d.Tasks {
			target := task.Ref
			if len(task.Composition) > 0 {
				target = strings.Join(task.Composition, ",")
			}
			fmt.Fprintf(w, "  %-15s %-40s %s\n", task.Name, target, task.Language)
			if len(task.Args) > 0 {
				fmt.Fprintf(w, "    requires args: %s\n", strings.Join(task.Args, ", "))
			}
			if len(task.Env) > 0 {
				fmt.Fprintf(w, "    requires env: %s\n", strings.Join(task.Env, ", "))
			}
			if task.Error != "" {
				fmt.Fprintf(w, "    ERROR: %s\n", task.Error)
			}
		}
		fmt.Fprintf(w, "\n%d tasks\n", len(d.Tasks))

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
