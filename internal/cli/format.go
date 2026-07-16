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
		ruleCounts := map[string]int{}

		for _, f := range resp.Findings {
			level := strings.ToUpper(f.Severity)
			loc := f.FilePath
			if f.Line > 0 {
				loc = fmt.Sprintf("%s:%d", f.FilePath, f.Line)
			}
			fmt.Fprintf(w, "%-5s %s  [%s] %s\n", level, loc, f.RuleID, f.Message)
			ruleCounts[f.RuleID]++

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
			// Engine warnings (rendered as NOTE lines below) are a routine class —
			// cache-unavailable, effect-pin batch-infra failures (U6) — distinct from
			// warn-severity findings. Surface their count so a run that quietly
			// degraded (e.g. pins unverified this run) is visible in the summary, not
			// only buried in the trailing NOTE lines. Omitted when zero to keep the
			// clean-run summary unchanged.
			if n := len(resp.Warnings); n > 0 {
				parts = append(parts, fmt.Sprintf("%d notes", n))
			}
			if resp.Stats.DurationMs > 0 {
				parts = append(parts, fmt.Sprintf("%dms", resp.Stats.DurationMs))
			}
			fmt.Fprintln(w, strings.Join(parts, ", "))
		} else if len(resp.Findings) == 0 {
			fmt.Fprintln(w, "clean")
		}

		// Per-rule breakdown: one line, descending count (ties by ID)
		if len(ruleCounts) > 0 {
			ids := make([]string, 0, len(ruleCounts))
			for id := range ruleCounts {
				ids = append(ids, id)
			}
			sort.Slice(ids, func(i, j int) bool {
				if ruleCounts[ids[i]] != ruleCounts[ids[j]] {
					return ruleCounts[ids[i]] > ruleCounts[ids[j]]
				}
				return ids[i] < ids[j]
			})
			perRule := make([]string, len(ids))
			for i, id := range ids {
				perRule[i] = fmt.Sprintf("%s=%d", id, ruleCounts[id])
			}
			fmt.Fprintf(w, "by rule: %s\n", strings.Join(perRule, " · "))
		}

		// Warnings (engine warnings, not findings)
		for _, w2 := range resp.Warnings {
			fmt.Fprintf(w, "NOTE  [%s] %s\n", w2.Code, w2.Message)
		}
		// A command may pair findings with a data payload (md def check:
		// verdict table + findings); render both rather than dropping the data.
		if resp.Data != nil {
			fmt.Fprintln(w)
			formatData(w, resp.Data)
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
		fmt.Fprintln(w, "\nParams are JSON: md <command> '{\"key\":\"value\"}'  (or `md <command> - < params.json`)")
		fmt.Fprintln(w, "Per-command help: md <command> --help")

	case HelpCommandData:
		fmt.Fprintf(w, "%s: %s\n", d.Command, d.Description)
		if d.Usage != "" {
			fmt.Fprintf(w, "\nUsage:\n  %s\n", d.Usage)
		}
		if len(d.Params) > 0 {
			fmt.Fprintln(w, "\nParameters:")
			names := make([]string, 0, len(d.Params))
			for name := range d.Params {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				p := d.Params[name]
				req := ""
				if p.Required {
					req = " (required)"
				}
				fmt.Fprintf(w, "  %-12s %s%s\n", name, p.Type, req)
			}
		}
		if len(d.ExitCodes) > 0 {
			fmt.Fprintln(w, "\nExit codes:")
			codes := make([]string, 0, len(d.ExitCodes))
			for code := range d.ExitCodes {
				codes = append(codes, code)
			}
			sort.Strings(codes)
			for _, code := range codes {
				fmt.Fprintf(w, "  %s  %s\n", code, d.ExitCodes[code])
			}
		}
		if len(d.Examples) > 0 {
			fmt.Fprintln(w, "\nExamples:")
			for _, ex := range d.Examples {
				fmt.Fprintf(w, "  %s\n", ex)
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

	case TocData:
		d.renderText(w)

	case DefCheckData:
		d.renderText(w)

	case AppendData:
		d.renderText(w)

	case EditSectionData:
		d.renderText(w)

	case ReadData:
		// stdout carries pure content (automation reads it); base/match
		// metadata goes to stderr from the handler. Separators only on
		// multi-match; a section read labels the separator with its hpath so
		// two sections from the same file stay distinguishable.
		if len(d.Matches) == 1 {
			io.WriteString(w, d.Matches[0].Content)
			return
		}
		for i, m := range d.Matches {
			if i > 0 {
				fmt.Fprintln(w)
			}
			label := m.Path
			if m.HPath != "" {
				label = m.Path + "#" + m.HPath
			}
			fmt.Fprintf(w, "--- %s ---\n", label)
			io.WriteString(w, m.Content)
		}

	case SkillRenderData:
		// stdout carries the rendered body only — a harness injects it
		// verbatim; skill/directive metadata goes to stderr from the handler.
		io.WriteString(w, d.Content)

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

	case CacheStatsData:
		fmt.Fprintf(w, "path:         %s\n", d.Path)
		fmt.Fprintf(w, "version dirs: %d\n", d.VersionDirs)
		fmt.Fprintf(w, "shards:       %d\n", d.Shards)
		fmt.Fprintf(w, "entries:      %d\n", d.Entries)
		fmt.Fprintf(w, "bytes:        %d\n", d.Bytes)

	case CacheCleanData:
		fmt.Fprintf(w, "removed %d entries / %d bytes\n", d.Entries, d.Bytes)
		fmt.Fprintf(w, "path: %s\n", d.Path)

	case CacheVerifyData:
		if d.Verified {
			fmt.Fprintf(w, "cache verified: %d entries checked, no divergence\n", d.EntriesChecked)
			return
		}
		fmt.Fprintf(w, "CACHE DIVERGENCE: %d entries checked, %d divergent findings\n", d.EntriesChecked, len(d.Divergences))
		for _, dv := range d.Divergences {
			loc := dv.FilePath
			if dv.Line > 0 {
				loc = fmt.Sprintf("%s:%d", dv.FilePath, dv.Line)
			}
			fmt.Fprintf(w, "  %-14s %s  [%s] %s\n", dv.Kind, loc, dv.RuleID, dv.Message)
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
			if len(task.Params) > 0 {
				fmt.Fprintf(w, "    accepts params: %s\n", strings.Join(task.Params, ", "))
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
