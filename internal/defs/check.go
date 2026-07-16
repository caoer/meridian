// check.go is the def-driven validator (schema-v2 §1.10 strata 1–3, static
// rungs). Verdicts are TRI-STATE: valid · legacy-useful · invalid. The law this
// file exists to hold (R6/C4, decision 7's "# Todo untouched"): a section
// present on disk but absent from the def's declared shape scores
// legacy-useful, NEVER invalid — and an entry that doesn't parse is flagged for
// form, kept for content. Only real violations (nested frontmatter, shape
// breaks, the terminal biconditional, guarded-empty sections at terminal) make
// a file invalid. Stratum 4 (# Checks md-run plug-ins) is U7.
package defs

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/body"
	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/types"
)

// Tri-state verdicts. Ordered: valid < legacy-useful < invalid.
const (
	VerdictValid   = "valid"
	VerdictLegacy  = "legacy-useful"
	VerdictInvalid = "invalid"
)

// SectionVerdict scores one on-disk section.
type SectionVerdict struct {
	Title   string `json:"title"`
	Verdict string `json:"verdict"`
	Note    string `json:"note,omitempty"`
}

// CensusEntry is one off-suggest value observation — a report that feeds
// vocabulary accretion (law 5), never a rejection.
type CensusEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Report is the outcome of checking one record against its resolved def.
type Report struct {
	Verdict  string
	Sections []SectionVerdict
	Census   []CensusEntry
	Findings []types.Finding
}

// Check validates doc against def: stratum 1 (property legality), stratum 2
// (cross-field shape), stratum 3 (section shape + entry grammar), plus the
// off-suggest census. It never mutates anything.
func Check(doc *body.Document, def *Def) *Report {
	rep := &Report{}
	path := doc.Path

	meta := recordMeta(doc, path, rep)
	if meta == nil {
		rep.Verdict = VerdictInvalid
		return rep
	}

	checkProperties(meta, def, path, rep)             // stratum 1
	terminal := checkCrossField(meta, def, path, rep) // stratum 2
	checkSections(doc, def, terminal, path, rep)      // stratum 3
	census(meta, def, path, rep)

	rep.Verdict = VerdictValid
	for _, f := range rep.Findings {
		if f.Severity == "error" {
			rep.Verdict = VerdictInvalid
			break
		}
	}
	if rep.Verdict != VerdictInvalid {
		for _, s := range rep.Sections {
			if s.Verdict == VerdictLegacy {
				rep.Verdict = VerdictLegacy
				break
			}
		}
	}
	return rep
}

// ScanNested reports the stratum-1 nested-frontmatter errors alone — the check
// that runs ALWAYS, even when no def resolves (fail-closed path included).
func ScanNested(doc *body.Document) []types.Finding {
	rep := &Report{}
	if meta := recordMeta(doc, doc.Path, rep); meta != nil {
		for _, key := range sortedKeys(meta) {
			if IsNested(meta[key]) {
				rep.Findings = append(rep.Findings, finding("def/nested-frontmatter", "error", doc.Path,
					fmt.Sprintf("%s: nested frontmatter is an ERROR always, from any writer (substrate law); flatten to a scalar or one-level list", key)))
			}
		}
	}
	return rep.Findings
}

// recordMeta decodes the record's frontmatter to a flat map. nil means the
// frontmatter itself is unreadable (finding already appended).
func recordMeta(doc *body.Document, path string, rep *Report) map[string]any {
	fm, err := frontmatter.ParseBytes(doc.Bytes())
	if err != nil || fm == nil {
		rep.Findings = append(rep.Findings, finding("def/frontmatter", "error", path,
			fmt.Sprintf("unreadable frontmatter: %v", err)))
		return nil
	}
	return fm.Meta
}

// checkProperties is stratum 1: flat YAML, known key, stable shape, required
// present. Nested is an error ALWAYS; an unknown scalar key is a warn
// (forward-compat, §1.10).
func checkProperties(meta map[string]any, def *Def, path string, rep *Report) {
	for _, key := range sortedKeys(meta) {
		v := meta[key]
		if IsNested(v) {
			rep.Findings = append(rep.Findings, finding("def/nested-frontmatter", "error", path,
				fmt.Sprintf("%s: nested frontmatter is an ERROR always, from any writer (substrate law); flatten to a scalar or one-level list", key)))
			continue
		}
		spec, known := def.Props[key]
		if !known {
			rep.Findings = append(rep.Findings, finding("def/unknown-key", "warn", path,
				fmt.Sprintf("%s: not declared by the %s def (forward-compat: kept, reported)", key, def.Kind)))
			continue
		}
		if v == nil {
			continue // present-but-empty = absent; required-ness below
		}
		if !CheckShape(spec.Shape, v) {
			rep.Findings = append(rep.Findings, finding("def/shape", "error", path,
				fmt.Sprintf("%s: value %v does not satisfy shape %s", key, v, spec.Shape)))
		}
	}
	for _, key := range sortedPropKeys(def.Props) {
		if def.Props[key].Required && meta[key] == nil {
			rep.Findings = append(rep.Findings, finding("def/required", "error", path,
				fmt.Sprintf("%s: required by the %s def, missing or empty", key, def.Kind)))
		}
	}
}

// checkCrossField is stratum 2: the terminal biconditional (status ∈ terminal
// ⟺ closed_at set), requires(<key>) guards, static section-non-empty guards
// (deferred to stratum 3 where the sections are), and owner/claims
// consistency. Returns whether the record sits at a terminal status.
func checkCrossField(meta map[string]any, def *Def, path string, rep *Report) bool {
	terminal := false
	if spec, ok := def.Props["status"]; ok && len(spec.Terminal) > 0 {
		status, _ := meta["status"].(string)
		for _, t := range spec.Terminal {
			if status == t {
				terminal = true
				break
			}
		}
		closed := meta["closed_at"] != nil
		if terminal && !closed {
			rep.Findings = append(rep.Findings, finding("def/biconditional", "error", path,
				fmt.Sprintf("status %q ∈ terminal %v but closed_at is empty (status ∈ terminal ⟺ closed_at set)", status, spec.Terminal)))
		}
		if !terminal && closed {
			rep.Findings = append(rep.Findings, finding("def/biconditional", "error", path,
				fmt.Sprintf("closed_at is set but status %q ∉ terminal %v (status ∈ terminal ⟺ closed_at set)", status, spec.Terminal)))
		}
	}

	for _, key := range sortedPropKeys(def.Props) {
		for _, g := range def.Props[key].Guard {
			kind, arg, _ := ParseGuard(g)
			if kind == "requires" && meta[key] != nil && meta[arg] == nil {
				rep.Findings = append(rep.Findings, finding("def/requires", "error", path,
					fmt.Sprintf("%s is set but requires(%s): %s is empty", key, arg, arg)))
			}
			// actor-not-owner and append-only need an actor and a prior state:
			// write-time guards, enforced when U7 wires I4 into Splice.
		}
	}

	// owner/claims consistency: owner empty, or matching the claim story
	// (§4.2 — a hand edit that sets owner directly is flagged, never absorbed).
	if _, hasOwner := def.Props["owner"]; hasOwner {
		if owner, _ := meta["owner"].(string); owner != "" {
			claims, _ := meta["claims"].([]any)
			last := ""
			if n := len(claims); n > 0 {
				last, _ = claims[n-1].(string)
			}
			if !strings.Contains(last, owner) {
				rep.Findings = append(rep.Findings, finding("def/owner-claims", "warn", path,
					fmt.Sprintf("owner %s does not match the last claims entry %q (hand-edited owner?)", owner, last)))
			}
		}
	}
	return terminal
}

var reBullet = regexp.MustCompile(`^- `)

// checkSections is stratum 3, walked over the body map in document order.
// Ternary routing per section: declared by name → scored against its rule;
// template-recognized → valid scaffold; anything else → legacy-useful, NEVER
// invalid. Subsections of a declared section are its entry content, not
// independently scored records.
func checkSections(doc *body.Document, def *Def, terminal bool, path string, rep *Report) {
	toc := doc.Toc()
	type span struct{ start, end int }
	var covered []span
	inCovered := func(s body.Section) bool {
		for _, c := range covered {
			if s.Start >= c.start && s.End <= c.end {
				return true
			}
		}
		return false
	}

	present := map[string]bool{}
	for _, sec := range toc.Sections {
		present[sec.Title] = true
		if inCovered(sec) {
			continue
		}
		if rule, declared := def.Section(sec.Title); declared {
			covered = append(covered, span{sec.Start, sec.End})
			rep.Sections = append(rep.Sections, scoreDeclared(doc, toc, sec, rule, terminal, path, rep))
			continue
		}
		if matchesTemplate(def.Template, sec) {
			rep.Sections = append(rep.Sections, SectionVerdict{Title: sec.Title, Verdict: VerdictValid, Note: "template scaffold"})
			continue
		}
		rep.Sections = append(rep.Sections, SectionVerdict{Title: sec.Title, Verdict: VerdictLegacy,
			Note: "not in the def's declared shape — kept untouched, never invalid"})
		rep.Findings = append(rep.Findings, findingAt("def/legacy-section", "warn", path, sec.StartLine-1,
			fmt.Sprintf("# %s: section absent from the %s def's shape → legacy-useful (decision 7: left untouched)", sec.Title, def.Kind)))
	}

	// Required scaffold: the literal template headings are what creation
	// instantiates; a missing one is repairable (insert-empty is within the
	// §1.10 repair boundary — U7's fix), so it warns, never invalidates.
	for _, p := range def.Template {
		if p.Literal && !present[p.Text] {
			rep.Findings = append(rep.Findings, finding("def/section-missing", "warn", path,
				fmt.Sprintf("# %s: template scaffold heading missing (repairable: `md def fix` inserts it empty)", p.Text)))
		}
	}
}

// scoreDeclared validates one declared section: emptiness at terminal, then
// the entry grammar. Grammar misses and legacy marks make the section
// legacy-useful — form flagged, content kept.
func scoreDeclared(doc *body.Document, toc body.Toc, sec body.Section, rule SectionRule, terminal bool, path string, rep *Report) SectionVerdict {
	sv := SectionVerdict{Title: sec.Title, Verdict: VerdictValid}

	if rule.RequiredBeforeTerminal && terminal &&
		len(strings.TrimSpace(string(doc.Source[sec.Start:sec.End]))) == 0 {
		rep.Findings = append(rep.Findings, findingAt("def/required-before-terminal", "error", path, sec.StartLine-1,
			fmt.Sprintf("# %s: must be non-empty before a terminal status (guard: section-non-empty)", sec.Title)))
		sv.Verdict = VerdictInvalid
		sv.Note = "empty at terminal status"
		return sv
	}
	if rule.Entry == "" {
		return sv
	}

	grammar := compileEntryRe(entryText(rule.Entry))
	legacy := 0
	miss := 0
	if depth := headingGrammarDepth(rule.Entry); depth > 0 {
		for _, sub := range toc.Sections {
			if sub.Start < sec.Start || sub.End > sec.End || sub.Depth != depth {
				continue
			}
			if rule.LegacyMark != "" && strings.Contains(sub.Title, rule.LegacyMark) {
				legacy++
				rep.Findings = append(rep.Findings, findingAt("def/legacy-entry", "warn", path, sub.StartLine-1,
					fmt.Sprintf("# %s: entry %q carries %s — kept for content, excluded from strict harvest", sec.Title, sub.Title, rule.LegacyMark)))
				continue
			}
			if !grammar.MatchString(sub.Title) {
				miss++
				rep.Findings = append(rep.Findings, findingAt("def/entry-grammar", "warn", path, sub.StartLine-1,
					fmt.Sprintf("# %s: entry %q does not parse as %q → would be tagged %s", sec.Title, sub.Title, rule.Entry, legacyMarkOr(rule))))
			}
		}
	} else {
		for _, ln := range sectionLines(doc.Source, sec) {
			text := strings.TrimRight(ln.text, " \t")
			if !reBullet.MatchString(text) {
				continue // continuation prose, comments, blanks: entry body, untouched
			}
			if rule.Sync != "" && strings.HasSuffix(text, "<!-- manual -->") {
				continue // the merge contract's manual lines are never grammar-bound
			}
			if !grammar.MatchString(text) {
				miss++
				rep.Findings = append(rep.Findings, findingAt("def/entry-grammar", "warn", path, ln.num,
					fmt.Sprintf("# %s: entry %q does not parse as %q → would be tagged %s", sec.Title, text, rule.Entry, legacyMarkOr(rule))))
			}
		}
	}
	if legacy+miss > 0 {
		sv.Verdict = VerdictLegacy
		sv.Note = fmt.Sprintf("%d legacy / %d unparsed entries", legacy, miss)
	}
	return sv
}

// census reports off-suggest values for every property that declares a
// suggest: list. Terminal values are the closed rung, never off-suggest. A
// census entry is information for vocabulary accretion — it never rejects.
func census(meta map[string]any, def *Def, path string, rep *Report) {
	for _, key := range sortedPropKeys(def.Props) {
		spec := def.Props[key]
		if len(spec.Suggest) == 0 {
			continue
		}
		v, _ := meta[key].(string)
		if v == "" || contains(spec.Suggest, v) || contains(spec.Terminal, v) {
			continue
		}
		rep.Census = append(rep.Census, CensusEntry{Key: key, Value: v})
		rep.Findings = append(rep.Findings, finding("def/census", "info", path,
			fmt.Sprintf("%s: %q is off-suggest %v (census: reported, never rejected)", key, v, spec.Suggest)))
	}
}

// --- helpers ---

// entryText strips the structural prefix of an entry grammar, leaving the text
// the grammar matches against: for "### {t}: {x}" the heading TITLE (the map
// already isolates it), for "- [...]" the whole line.
func entryText(entry string) string {
	if d := headingGrammarDepth(entry); d > 0 {
		return strings.TrimSpace(entry[d:])
	}
	return entry
}

// headingGrammarDepth returns the heading depth of an entry grammar ("###..."),
// or 0 for a line grammar.
func headingGrammarDepth(entry string) int {
	d := 0
	for d < len(entry) && entry[d] == '#' {
		d++
	}
	if d > 0 && d < len(entry) && entry[d] == ' ' {
		return d
	}
	return 0
}

func legacyMarkOr(rule SectionRule) string {
	if rule.LegacyMark != "" {
		return rule.LegacyMark
	}
	return "#legacy"
}

type numberedLine struct {
	text string
	num  int
}

// sectionLines yields a section's content lines with whole-file line numbers,
// fence-aware so fenced payloads inside entries are never scanned as entries.
func sectionLines(src []byte, sec body.Section) []numberedLine {
	var out []numberedLine
	inFence := false
	num := sec.StartLine
	for _, ln := range strings.Split(string(src[sec.Start:sec.End]), "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			num++
			continue
		}
		if !inFence {
			out = append(out, numberedLine{text: ln, num: num})
		}
		num++
	}
	return out
}

func matchesTemplate(patterns []TemplatePattern, sec body.Section) bool {
	for _, p := range patterns {
		if p.Match(sec.Depth, sec.Title) {
			return true
		}
	}
	return false
}

func finding(rule, severity, path, msg string) types.Finding {
	return types.Finding{RuleID: rule, Severity: severity, FilePath: path, Message: msg}
}

func findingAt(rule, severity, path string, line int, msg string) types.Finding {
	return types.Finding{RuleID: rule, Severity: severity, FilePath: path, Line: line, Message: msg}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedPropKeys(m map[string]PropSpec) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
