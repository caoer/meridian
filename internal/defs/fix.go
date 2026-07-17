// fix.go plans `md def fix` — the repair rung run deliberately, CHECK-MOSTLY in
// v1 (R-scope, adversarial F7). What it plans as edits, all within the §1.10
// repair boundary:
//
//   - stamp derived fields: a stamp:close property (canonically closed_at) empty
//     while status sits at a terminal value;
//   - default tags: a missing/empty `tags` key filled with the def's default;
//   - legacy marks: declared-section entries that fail their entry grammar get
//     the rule's legacy mark appended (kept for content, excluded from strict
//     harvest) — the mark, never a rewrite;
//   - the legacy-Todo marker: a POPULATED legacy `# Todo` gets one marker comment
//     prepended so a human reading the file knows `# Tasks` (the cc-tasks sync
//     mirror) superseded it (design-lens-2). The census surfaces these files
//     fleet-wide regardless of the marker.
//
// What it only REPORTS, never writes (v1 deferrals, noted per the task):
//
//   - missing template scaffold sections — "missing section X" is reported, NOT
//     scaffolded into live files; structure scaffolding is deferred until the
//     section model is proven (R-scope);
//   - a tags list present but missing the default entry (a block-style list
//     cannot be rewritten through the single-line property plane without
//     corrupting it — report, don't guess);
//   - grammar misses whose line cannot be safely marked (ambiguous anchor, CRLF).
//
// NEVER: body content, domain facts (status, owner, verdict, answer), section
// structure. The plan executes through ONE body.Splice batch, so I0/I3/I4 apply
// — fixing another agent's file is refused by policy naming the owner, and fix
// is idempotent by construction (a second plan over the fixed file is empty).
package defs

import (
	"fmt"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/body"
	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/types"
)

// LegacyTodoMarker is the comment `md def fix` prepends into a populated legacy
// `# Todo` section. Presence of the marker is the idempotency key.
const LegacyTodoMarker = "<!-- md:legacy — populated # Todo is a legacy section (the # Tasks sync mirror supersedes it); content kept, excluded from strict harvest -->"

// FixPlan is one file's planned repair: the edits to splice (empty on a clean or
// already-fixed file), a human line per edit, and the check-only findings.
type FixPlan struct {
	Edits    []body.Edit
	Actions  []string
	Reported []types.Finding
}

// PlanFix computes the fix plan for doc against def. It never writes; the caller
// splices plan.Edits (one batch) and surfaces plan.Reported.
func PlanFix(doc *body.Document, def *Def) *FixPlan {
	plan := &FixPlan{}
	planStamps(doc, def, plan)
	planDefaultTags(doc, def, plan)
	planLegacyMarks(doc, def, plan)
	planTodoMarker(doc, def, plan)
	reportMissingSections(doc, def, plan)
	return plan
}

// planStamps adds the stamp:close autofill (same rule as the I4 repair rung).
func planStamps(doc *body.Document, def *Def, plan *FixPlan) {
	for _, e := range planCloseStamps(doc, def) {
		plan.Edits = append(plan.Edits, e)
		plan.Actions = append(plan.Actions, fmt.Sprintf("stamp %s: %s (status is terminal; stamp: close)", e.Target, e.New))
	}
}

// planDefaultTags fills a missing/empty `tags` key with the def's declared
// default, rendered as a one-line flow list. A tags list that EXISTS but lacks
// the default entry is reported only: the property plane is single-line, and
// rewriting a block-style list through it would corrupt the frontmatter.
func planDefaultTags(doc *body.Document, def *Def, plan *FixPlan) {
	spec, ok := def.Props["tags"]
	if !ok || spec.Default == nil {
		return
	}
	defaults, ok := spec.Default.([]any)
	if !ok || len(defaults) == 0 {
		return
	}
	fm, err := frontmatter.ParseBytes(doc.Bytes())
	if err != nil || fm == nil {
		return
	}
	var wants []string
	for _, d := range defaults {
		if s, ok := d.(string); ok {
			wants = append(wants, s)
		}
	}
	if len(wants) == 0 {
		return
	}
	cur, has := fm.Meta["tags"]
	if !has || cur == nil {
		val := "[" + strings.Join(wants, ", ") + "]"
		plan.Edits = append(plan.Edits, body.Edit{Op: body.OpSetProperty, Target: "tags", New: val})
		plan.Actions = append(plan.Actions, "set default tags: "+val)
		return
	}
	if items, ok := cur.([]any); ok {
		var have []string
		for _, it := range items {
			if s, ok := it.(string); ok {
				have = append(have, s)
			}
		}
		for _, w := range wants {
			if !contains(have, w) {
				plan.Reported = append(plan.Reported, finding("def/default-tag", "warn", doc.Path,
					fmt.Sprintf("tags: default %q missing from %v — reported, not rewritten (a multi-line tags list cannot be safely edited through the single-line property plane)", w, have)))
			}
		}
	}
}

// planLegacyMarks appends the rule's legacy mark to declared-section entries
// that fail their entry grammar — heading entries and line entries alike. An
// entry whose exact bytes are not unique within the section, or that carries a
// CR, is reported instead of marked (never guess at an anchor).
func planLegacyMarks(doc *body.Document, def *Def, plan *FixPlan) {
	toc := doc.Toc()
	lines := strings.Split(string(doc.Source), "\n")
	for _, sec := range toc.Sections {
		rule, declared := def.Section(sec.Title)
		if !declared || rule.Entry == "" {
			continue
		}
		mark := legacyMarkOr(rule)
		grammar := compileEntryRe(entryText(rule.Entry))
		if depth := headingGrammarDepth(rule.Entry); depth > 0 {
			for _, sub := range toc.Sections {
				if sub.Start < sec.Start || sub.End > sec.End || sub.Depth != depth {
					continue
				}
				if strings.Contains(sub.Title, mark) || grammar.MatchString(sub.Title) {
					continue
				}
				raw := ""
				if n := sub.StartLine - 2; n >= 0 && n < len(lines) {
					raw = lines[n] // the heading line sits one line above the content span
				}
				planMark(doc, sec, raw, mark, plan)
			}
			continue
		}
		for _, ln := range sectionLines(doc.Source, sec) {
			text := strings.TrimRight(ln.text, " \t")
			if !reBullet.MatchString(text) {
				continue
			}
			if rule.Sync != "" && strings.HasSuffix(text, "<!-- manual -->") {
				continue
			}
			if strings.Contains(text, mark) || grammar.MatchString(text) {
				continue
			}
			planMark(doc, sec, ln.text, mark, plan)
		}
	}
}

// planMark plans one "append the mark to this exact line" edit, or reports why
// it cannot be done safely.
func planMark(doc *body.Document, sec body.Section, raw, mark string, plan *FixPlan) {
	if raw == "" || strings.ContainsRune(raw, '\r') {
		plan.Reported = append(plan.Reported, finding("def/legacy-mark", "warn", doc.Path,
			fmt.Sprintf("# %s: entry %q needs the %s mark but cannot be marked safely — mark it by hand", sec.Title, raw, mark)))
		return
	}
	content := string(doc.Source[sec.Start:sec.End])
	if strings.Count(content, raw) != 1 {
		plan.Reported = append(plan.Reported, finding("def/legacy-mark", "warn", doc.Path,
			fmt.Sprintf("# %s: entry %q is not a unique anchor in its section — mark it by hand", sec.Title, raw)))
		return
	}
	trimmed := strings.TrimRight(raw, " \t")
	plan.Edits = append(plan.Edits, body.Edit{
		Op:     body.OpReplace,
		Target: sec.HPath,
		Find:   raw,
		New:    trimmed + " " + mark,
	})
	plan.Actions = append(plan.Actions, fmt.Sprintf("mark legacy entry in # %s: %q + %s", sec.Title, trimmed, mark))
}

// planTodoMarker inserts the LegacyTodoMarker at the head of a populated legacy
// `# Todo` (a Todo section the def does not declare). Empty Todo sections and
// already-marked ones are left alone. The edit anchors on the section's full
// current content (unique by construction) and re-writes it byte-identically
// with the marker line prepended, so the content itself never changes.
func planTodoMarker(doc *body.Document, def *Def, plan *FixPlan) {
	for _, sec := range doc.Toc().Sections {
		if sec.Title != "Todo" {
			continue
		}
		if _, declared := def.Section(sec.Title); declared || matchesTemplate(def.Template, sec) {
			continue
		}
		content := string(doc.Source[sec.Start:sec.End])
		if strings.TrimSpace(content) == "" || strings.Contains(content, LegacyTodoMarker) {
			continue
		}
		plan.Edits = append(plan.Edits, body.Edit{
			Op:     body.OpReplace,
			Target: sec.HPath,
			Find:   content,
			New:    LegacyTodoMarker + "\n" + content,
		})
		plan.Actions = append(plan.Actions, "insert legacy marker comment into populated # "+sec.HPath)
	}
}

// reportMissingSections reports template scaffold headings absent from the file
// — REPORTED, never scaffolded (v1 defers structure scaffolding until the
// section model is proven; R-scope).
func reportMissingSections(doc *body.Document, def *Def, plan *FixPlan) {
	present := map[string]bool{}
	for _, sec := range doc.Toc().Sections {
		present[sec.Title] = true
	}
	var missing []string
	for _, p := range def.Template {
		if p.Literal && !present[p.Text] {
			missing = append(missing, p.Text)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		plan.Reported = append(plan.Reported, finding("def/section-missing", "warn", doc.Path,
			fmt.Sprintf("missing section # %s — reported, NOT scaffolded (v1 defers structure scaffolding into live files until the section model is proven; R-scope)", name)))
	}
}
