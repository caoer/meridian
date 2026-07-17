package defs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/body"
	"github.com/caoer/meridian/internal/types"
)

// repoDefs is the layer list for the authored gate defs at the repo root.
func repoDefs(t *testing.T) []string {
	t.Helper()
	return []string{"../../defs"}
}

func loadGolden(t *testing.T, name string) *body.Document {
	t.Helper()
	doc, err := body.Load(filepath.Join("../body/testdata/demo", name))
	if err != nil {
		t.Fatalf("load golden %s: %v", name, err)
	}
	return doc
}

func checkGolden(t *testing.T, file, kind, preset string) *Report {
	t.Helper()
	def, err := Resolve(kind, preset, repoDefs(t))
	if err != nil {
		t.Fatalf("resolve def %s: %v", kind, err)
	}
	return Check(loadGolden(t, file), def)
}

func sectionVerdict(t *testing.T, rep *Report, title string) SectionVerdict {
	t.Helper()
	for _, s := range rep.Sections {
		if s.Title == title {
			return s
		}
	}
	t.Fatalf("no section verdict for %q in %+v", title, rep.Sections)
	return SectionVerdict{}
}

func errorFindings(rep *Report) []types.Finding {
	var out []types.Finding
	for _, f := range rep.Findings {
		if f.Severity == "error" {
			out = append(out, f)
		}
	}
	return out
}

// --- THE GATE: the five goldens validate exactly as schema-v2 §2 claims ---

func TestGoldenAgent(t *testing.T) {
	rep := checkGolden(t, "agent.md", "agent", "")
	if errs := errorFindings(rep); len(errs) != 0 {
		t.Fatalf("agent golden must carry zero errors, got %+v", errs)
	}
	// One #legacy Memo entry → the file is legacy-useful, NEVER invalid.
	if rep.Verdict != VerdictLegacy {
		t.Fatalf("agent verdict = %q, want %q (one #legacy memo entry)", rep.Verdict, VerdictLegacy)
	}
	for title, want := range map[string]string{
		"Tasks":   VerdictValid, // incl. the <!-- manual --> line, exempt under sync
		"Memo":    VerdictLegacy,
		"Notes":   VerdictValid,
		"Handoff": VerdictValid,
	} {
		if got := sectionVerdict(t, rep, title).Verdict; got != want {
			t.Errorf("agent # %s = %q, want %q", title, got, want)
		}
	}
	// Off-suggest census: the free-form status is reported, never rejected.
	if len(rep.Census) != 1 || rep.Census[0].Key != "status" {
		t.Fatalf("agent census = %+v, want one off-suggest status entry", rep.Census)
	}
}

func TestGoldenTask(t *testing.T) {
	rep := checkGolden(t, "task.md", "task", "")
	if rep.Verdict != VerdictValid {
		t.Fatalf("task verdict = %q, want valid; findings: %+v", rep.Verdict, rep.Findings)
	}
	// retired ∈ terminal with closed_at set, owner released, claims history kept.
	for _, title := range []string{"Objective", "Context", "Gate Evidence", "Activity"} {
		if got := sectionVerdict(t, rep, title).Verdict; got != VerdictValid {
			t.Errorf("task ## %s = %q, want valid", title, got)
		}
	}
	if len(rep.Census) != 0 {
		t.Errorf("retired is terminal, not off-suggest; census = %+v", rep.Census)
	}
}

func TestGoldenCard(t *testing.T) {
	rep := checkGolden(t, "card.md", "card", "")
	if rep.Verdict != VerdictValid {
		t.Fatalf("card verdict = %q, want valid; findings: %+v", rep.Verdict, rep.Findings)
	}
	// answered ∈ terminal ⟺ closed_at set; answer satisfies requires(answered).
	for _, title := range []string{"Options", "Answer"} {
		if got := sectionVerdict(t, rep, title).Verdict; got != VerdictValid {
			t.Errorf("card ## %s = %q, want valid", title, got)
		}
	}
}

func TestGoldenMemo(t *testing.T) {
	rep := checkGolden(t, "memo.md", "memo", "")
	if rep.Verdict != VerdictValid {
		t.Fatalf("memo verdict = %q, want valid; findings: %+v", rep.Verdict, rep.Findings)
	}
	for _, title := range []string{"Claim", "Evidence", "Consequence"} {
		if got := sectionVerdict(t, rep, title).Verdict; got != VerdictValid {
			t.Errorf("memo ## %s = %q, want valid", title, got)
		}
	}
}

func TestGoldenSessionStandard(t *testing.T) {
	rep := checkGolden(t, "session-standard.md", "session", "standard")
	if rep.Verdict != VerdictValid {
		t.Fatalf("session verdict = %q, want valid; findings: %+v", rep.Verdict, rep.Findings)
	}
	for _, title := range []string{"Board", "Agents", "Log"} {
		if got := sectionVerdict(t, rep, title).Verdict; got != VerdictValid {
			t.Errorf("session # %s = %q, want valid", title, got)
		}
	}
	// The record-title heading is template scaffold, not legacy.
	if got := sectionVerdict(t, rep, "QDC507 EDL Recovery").Verdict; got != VerdictValid {
		t.Errorf("session title heading = %q, want valid (template scaffold)", got)
	}
}

// --- R6/C4: legacy-useful, never invalid (decision 7's "# Todo untouched") ---

func TestOffDefSectionScoresLegacyUsefulNeverInvalid(t *testing.T) {
	src, err := os.ReadFile("../body/testdata/demo/agent.md")
	if err != nil {
		t.Fatal(err)
	}
	withTodo := string(src) + "\n# Todo\n- [ ] a populated legacy todo kept from the pre-v2 layout\n"
	doc, err := body.Parse([]byte(withTodo))
	if err != nil {
		t.Fatal(err)
	}
	def, err := Resolve("agent", "", repoDefs(t))
	if err != nil {
		t.Fatal(err)
	}
	rep := Check(doc, def)

	todo := sectionVerdict(t, rep, "Todo")
	if todo.Verdict != VerdictLegacy {
		t.Fatalf("# Todo = %q, want %q — a section absent from the def is NEVER invalid", todo.Verdict, VerdictLegacy)
	}
	if rep.Verdict == VerdictInvalid {
		t.Fatalf("file verdict = invalid; off-def sections must not invalidate (findings: %+v)", rep.Findings)
	}
	if errs := errorFindings(rep); len(errs) != 0 {
		t.Fatalf("off-def section produced error findings: %+v", errs)
	}
}

// --- Stratum 1: nested frontmatter is an ERROR always ---

func TestNestedFrontmatterErrors(t *testing.T) {
	doc, err := body.Parse([]byte(`---
type: task
created: 2026-07-15T07:02:11
session: s
status: todo
retry: {max: 3, backoff: exp}
tags: [type/task]
---

# Task: x

## Objective
`))
	if err != nil {
		t.Fatal(err)
	}
	def, err := Resolve("task", "", repoDefs(t))
	if err != nil {
		t.Fatal(err)
	}
	rep := Check(doc, def)
	if rep.Verdict != VerdictInvalid {
		t.Fatalf("nested frontmatter must be invalid, got %q", rep.Verdict)
	}
	found := false
	for _, f := range rep.Findings {
		if f.RuleID == "def/nested-frontmatter" && f.Severity == "error" && strings.Contains(f.Message, "retry") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no def/nested-frontmatter error for `retry`; findings: %+v", rep.Findings)
	}

	// The scan holds even with no def resolved (fail-closed path).
	if fs := ScanNested(doc); len(fs) != 1 || fs[0].RuleID != "def/nested-frontmatter" {
		t.Fatalf("ScanNested = %+v, want the retry error", fs)
	}
}

// --- Stratum 2: the terminal biconditional, both directions ---

func TestBiconditional(t *testing.T) {
	def, err := Resolve("task", "", repoDefs(t))
	if err != nil {
		t.Fatal(err)
	}
	base := `---
type: task
created: 2026-07-15T07:02:11
session: s
status: %s
closed_at: %s
tags: [type/task]
---

# Task: x
`
	for _, tc := range []struct {
		name, status, closed string
		wantInvalid          bool
	}{
		{"terminal-without-closed_at", "done", "", true},
		{"closed_at-without-terminal", "in-progress", "2026-07-15T11:41:09", true},
		{"terminal-with-closed_at", "retired", "2026-07-15T11:41:09", false},
		{"open-without-closed_at", "todo", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(base, "%s", tc.status, 1)
			src = strings.Replace(src, "%s", tc.closed, 1)
			doc, err := body.Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			rep := Check(doc, def)
			gotInvalid := rep.Verdict == VerdictInvalid
			if gotInvalid != tc.wantInvalid {
				t.Fatalf("verdict = %q (invalid=%v), want invalid=%v; findings %+v",
					rep.Verdict, gotInvalid, tc.wantInvalid, rep.Findings)
			}
		})
	}
}

// --- Guards: requires(<key>) static evaluation ---

func TestRequiresGuard(t *testing.T) {
	def, err := Resolve("card", "", repoDefs(t))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := body.Parse([]byte(`---
type: card
asked-by: "[[9c31d0a2]]"
urgency: block
status: open
asked: 2026-07-15T10:52:07
answer: "a"
tags: [type/card]
---

# A question?

## Options
- a
`))
	if err != nil {
		t.Fatal(err)
	}
	rep := Check(doc, def)
	found := false
	for _, f := range rep.Findings {
		if f.RuleID == "def/requires" && f.Severity == "error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("answer without answered must fail requires(answered); findings: %+v", rep.Findings)
	}
}

// --- Guard: required-before-terminal (section-non-empty as section rule) ---

func TestRequiredBeforeTerminal(t *testing.T) {
	def, err := Resolve("agent", "", repoDefs(t))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := body.Parse([]byte(`---
type: agent
role: worker
claude-session-id: x
host: h
launched-via: "tmux:%1"
created: 2026-07-15T08:12:40
manifest: "m"
status: terminated
closed_at: 2026-07-15T18:00:00
tags: [type/agent]
---

# Tasks

# Memo

# Notes

# Handoff
`))
	if err != nil {
		t.Fatal(err)
	}
	rep := Check(doc, def)
	if rep.Verdict != VerdictInvalid {
		t.Fatalf("terminated with empty # Handoff must be invalid, got %q; findings %+v", rep.Verdict, rep.Findings)
	}
	if got := sectionVerdict(t, rep, "Handoff").Verdict; got != VerdictInvalid {
		t.Fatalf("# Handoff = %q, want invalid at terminal", got)
	}
}

// --- The 9 shapes, closed ---

func TestShapes(t *testing.T) {
	iso := "2026-07-15T08:12:40"
	for _, tc := range []struct {
		shape string
		ok    []any
		bad   []any
	}{
		{ShapeLine, []any{"one line"}, []any{"two\nlines", 3, true}},
		{ShapeText, []any{"multi\nline", "one"}, []any{7, false}},
		{ShapeISO, []any{iso, "2026-07-15T08:12"}, []any{"yesterday", 5}},
		{ShapeInt, []any{42}, []any{"42", 4.2}},
		{ShapeBool, []any{true}, []any{"true", 1}},
		{ShapeRef, []any{"[[edl-flash-system-only]]", "[[MISSION#Recovery]]"}, []any{"edl", "[[]]", 1}},
		{ShapeListLine, []any{[]any{"a", "b"}, []any{}}, []any{"a", []any{1}}},
		{ShapeListRef, []any{[]any{"[[a]]"}, []any{}}, []any{[]any{"a"}, "[[a]]"}},
		{ShapeListISO, []any{[]any{iso}}, []any{[]any{"x"}, iso}},
	} {
		for _, v := range tc.ok {
			if !CheckShape(tc.shape, v) {
				t.Errorf("%s should accept %#v", tc.shape, v)
			}
		}
		for _, v := range tc.bad {
			if CheckShape(tc.shape, v) {
				t.Errorf("%s should reject %#v", tc.shape, v)
			}
		}
	}
	for _, s := range []string{"line", "text", "iso", "int", "bool", "ref", "list(line)", "list(ref)", "list(iso)"} {
		if !ValidShape(s) {
			t.Errorf("closed set must include %q", s)
		}
	}
	for _, s := range []string{"list(text)", "list(list(line))", "map", "enum", ""} {
		if ValidShape(s) {
			t.Errorf("closed set must exclude %q", s)
		}
	}
}

// --- Cascade: session → preset → builtin, nearest wins PER KEY ---

func TestCascadeNearestWinsPerKey(t *testing.T) {
	near := t.TempDir()
	sessionDef := `---
type: def
defines: task
version: 7
tags: [type/def]
---

# What a task is (session override)

# Properties

` + "```yaml" + `
status: {shape: line, required: true, default: idea, suggest: [idea, cooking], terminal: [shipped]}
` + "```" + `
^properties
`
	if err := os.WriteFile(filepath.Join(near, "task.md"), []byte(sessionDef), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := Resolve("task", "", append([]string{near}, repoDefs(t)...))
	if err != nil {
		t.Fatal(err)
	}
	// Nearest wins for the overridden key…
	if got := def.Props["status"].Terminal; len(got) != 1 || got[0] != "shipped" {
		t.Fatalf("status.terminal = %v, want [shipped] (nearest layer)", got)
	}
	// …while every other key falls through to the builtin.
	if got := def.Props["owner"].Shape; got != ShapeRef {
		t.Fatalf("owner.shape = %q, want ref (builtin layer)", got)
	}
	if len(def.Sections) == 0 || def.Version != 7 {
		t.Fatalf("sections/template must fall through, version nearest: v=%d sections=%d", def.Version, len(def.Sections))
	}
}

// --- Malformed def → fail closed ---

func TestMalformedDefFailsClosed(t *testing.T) {
	dir := t.TempDir()
	for name, blob := range map[string]string{
		// unknown spec key
		"a": "status: {shape: line, colour: red}",
		// nested param
		"b": "status: {shape: line, retry: {max: 3}}",
		// off-vocabulary shape
		"c": "status: {shape: enum}",
		// off-vocabulary guard
		"d": "status: {shape: line, guard: [owner-must-sign]}",
	} {
		def := `---
type: def
defines: widget
version: 1
tags: [type/def]
---

# What a widget is

# Properties

` + "```yaml\n" + blob + "\n```" + `
^properties
`
		if err := os.WriteFile(filepath.Join(dir, "widget.md"), []byte(def), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Resolve("widget", "", []string{dir}); err == nil {
			t.Errorf("case %s: malformed def must fail closed, resolved fine", name)
		} else if !strings.Contains(err.Error(), "INVALID_PARAMS") {
			t.Errorf("case %s: error should name INVALID_PARAMS, got: %v", name, err)
		}
	}
}

// --- Entry grammar: unparseable entries flag form, keep content ---

func TestEntryGrammarMissIsLegacyNeverInvalid(t *testing.T) {
	def, err := Resolve("agent", "", repoDefs(t))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := body.Parse([]byte(`---
type: agent
role: worker
claude-session-id: x
host: h
launched-via: "tmux:%1"
created: 2026-07-15T08:12:40
manifest: "m"
status: working
closed_at:
tags: [type/agent]
---

# Tasks

# Memo
### this heading has no memo-type prefix
prose

# Notes

# Handoff
todo
`))
	if err != nil {
		t.Fatal(err)
	}
	rep := Check(doc, def)
	if got := sectionVerdict(t, rep, "Memo").Verdict; got != VerdictLegacy {
		t.Fatalf("# Memo with unparseable entry = %q, want legacy-useful", got)
	}
	if rep.Verdict != VerdictLegacy {
		t.Fatalf("file = %q, want legacy-useful (form flagged, content kept)", rep.Verdict)
	}
}

// TestLineGrammarLegacyMarkExempt pins the U7 completion of the legacy-mark
// exemption: a MARKED line entry (bullet grammar) scores legacy-useful and is
// NOT re-flagged as a grammar miss — the symmetry with heading entries that
// makes `md def fix` idempotent and its own writes conformant.
func TestLineGrammarLegacyMarkExempt(t *testing.T) {
	dir := t.TempDir()
	defSrc := `---
type: def
defines: logbook
version: 1
tags: [type/def]
---

# What a logbook is

# Properties

` + "```yaml" + `
type: {shape: line, required: true, default: logbook}
` + "```" + `
^properties

# Sections

## section: Log
` + "```yaml" + `
entry: "- {iso} {line}"
legacy-mark: "#legacy"
` + "```" + `
`
	if err := os.WriteFile(filepath.Join(dir, "logbook.md"), []byte(defSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := filepath.Join(dir, "r.md")
	src := `---
type: logbook
---

# Log

- 2026-07-16T10:00 parses fine
- freeform relic line #legacy
- relic
`
	if err := os.WriteFile(rec, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	def, err := Resolve("logbook", "", []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := body.Load(rec)
	if err != nil {
		t.Fatal(err)
	}
	rep := Check(doc, def)
	var marked, missed int
	for _, f := range rep.Findings {
		switch f.RuleID {
		case "def/legacy-entry":
			marked++
			if !strings.Contains(f.Message, "#legacy") {
				t.Errorf("legacy-entry finding must name the mark: %s", f.Message)
			}
		case "def/entry-grammar":
			missed++
			if strings.Contains(f.Message, "freeform relic line") {
				t.Errorf("a marked line must NOT be re-flagged as a grammar miss: %s", f.Message)
			}
		}
	}
	if marked != 1 || missed != 1 {
		t.Fatalf("findings: %d legacy / %d missed, want 1/1; all: %+v", marked, missed, rep.Findings)
	}
	if sv := sectionVerdict(t, rep, "Log"); sv.Verdict != VerdictLegacy {
		t.Fatalf("# Log verdict = %s, want legacy-useful", sv.Verdict)
	}
}
