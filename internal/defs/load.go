// load.go loads a defs/<kind>.md definition file THROUGH the body map (never a
// private markdown re-parse): the ^properties yaml block via the block index,
// the `## section: <Name>` rules via the section table, the ^template block via
// the block index. Decoding is STRICT — an unknown or nested param anywhere in
// the machine-readable blocks is INVALID_PARAMS, and a malformed def fails
// CLOSED: the caller gets an error and validates nothing against a half-read
// schema (findings only, auto-mutation disabled).
package defs

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/body"
	"github.com/caoer/meridian/internal/frontmatter"
	"go.yaml.in/yaml/v3"
)

// PropSpec is one property declaration in a def's ^properties block — the
// closed declaration syntax of schema-v2 §1.2:
//
//	<key>: {shape: <shape>, required?: bool, default?: scalar, suggest?: [..],
//	        terminal?: [..], stamp?: create|touch|close, guard?: [..]}
//
// Strict-decoded: any key outside this set is INVALID_PARAMS.
type PropSpec struct {
	Shape    string   `yaml:"shape"`
	Required bool     `yaml:"required"`
	Default  any      `yaml:"default"`
	Suggest  []string `yaml:"suggest"`  // soft rung: autocomplete/census only, never rejects
	Terminal []string `yaml:"terminal"` // hard rung: the only closed/enforced part
	Stamp    string   `yaml:"stamp"`
	Guard    []string `yaml:"guard"`
}

// SectionRule is one `## section: <Name>` rule block — the closed section
// vocabulary of schema-v2 §1.3. Strict-decoded.
type SectionRule struct {
	Write                  string `yaml:"write"`
	Entry                  string `yaml:"entry"`
	Sync                   string `yaml:"sync"`
	Merge                  string `yaml:"merge"`
	Collision              string `yaml:"collision"`
	RequiredBeforeTerminal bool   `yaml:"required-before-terminal"`
	PromoteAt              string `yaml:"promote-at"`
	LegacyMark             string `yaml:"legacy-mark"`
	OnViolation            string `yaml:"on-violation"`
}

// DefSection pairs a declared section name with its rule, in def order.
type DefSection struct {
	Name string
	Rule SectionRule
}

// TemplatePattern is one heading line of the ^template block, compiled so a
// record heading can be recognized as template scaffold: literal text anchored,
// {{var}} placeholders wildcarded. The record-title heading (e.g. the card's
// free-text question under `# {{question}}`) is template-recognized this way —
// part of the declared shape, never scored legacy.
type TemplatePattern struct {
	Depth   int
	Text    string // heading text as written in the template
	Literal bool   // no {{var}} — a required scaffold heading
	re      *regexp.Regexp
}

// Match reports whether a record heading (depth + title) is this template line.
func (p TemplatePattern) Match(depth int, title string) bool {
	return depth == p.Depth && p.re.MatchString(title)
}

// Def is one loaded (or cascade-merged) definition.
type Def struct {
	Kind     string
	Preset   string
	Version  int
	Props    map[string]PropSpec
	Sections []DefSection
	Template []TemplatePattern
	Sources  []string // def file(s) this was resolved from, nearest first
}

// Section returns the rule declared for a section name.
func (d *Def) Section(name string) (SectionRule, bool) {
	for _, s := range d.Sections {
		if s.Name == name {
			return s.Rule, true
		}
	}
	return SectionRule{}, false
}

var (
	reHeadingLine = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*$`)
	reTemplateVar = regexp.MustCompile(`\{\{[^{}]+\}\}`) // template placeholders: {{var}}
	reEntryVar    = regexp.MustCompile(`\{[^{}]+\}`)     // entry-grammar placeholders: {var}
)

// LoadDef loads one definition file through the body map.
func LoadDef(path string) (*Def, error) {
	doc, err := body.Load(path)
	if err != nil {
		return nil, fmt.Errorf("def %s: %w", path, err)
	}

	fm, err := frontmatter.ParseBytes(doc.Bytes())
	if err != nil || fm == nil {
		return nil, fmt.Errorf("def %s: unreadable frontmatter: %v", path, err)
	}
	if fm.StringField("type") != "def" {
		return nil, fmt.Errorf("def %s: type is %q, want \"def\"", path, fm.StringField("type"))
	}
	def := &Def{
		Kind:    fm.StringField("defines"),
		Preset:  fm.StringField("preset"),
		Props:   map[string]PropSpec{},
		Sources: []string{path},
	}
	if def.Kind == "" {
		return nil, fmt.Errorf("def %s: missing `defines:` kind", path)
	}
	if v, ok := fm.Meta["version"].(int); ok {
		def.Version = v
	}

	// ^properties — strict decode; ALL params top-level, never nested. A nested
	// or unknown param is INVALID_PARAMS (stratum-1 law applied to the def's own
	// machine block).
	propSrc, rerr := doc.Read("^properties")
	if rerr != nil {
		return nil, fmt.Errorf("def %s: no ^properties block: %v", path, rerr)
	}
	if err := strictYAML(stripFence(propSrc.Content), &def.Props); err != nil {
		return nil, fmt.Errorf("def %s: INVALID_PARAMS in ^properties: %v", path, err)
	}
	for key, spec := range def.Props {
		if !ValidShape(spec.Shape) {
			return nil, fmt.Errorf("def %s: INVALID_PARAMS: %s: unknown shape %q (closed set: line text iso int bool ref list(line) list(ref) list(iso))", path, key, spec.Shape)
		}
		if !validStamps[spec.Stamp] {
			return nil, fmt.Errorf("def %s: INVALID_PARAMS: %s: unknown stamp %q (closed set: create touch close)", path, key, spec.Stamp)
		}
		for _, g := range spec.Guard {
			if _, _, ok := ParseGuard(g); !ok {
				return nil, fmt.Errorf("def %s: INVALID_PARAMS: %s: unknown guard %q (closed set: actor-not-owner section-non-empty(<S>) requires(<key>) append-only)", path, key, g)
			}
		}
	}

	// `## section: <Name>` rule blocks, in def order, via the section table.
	for _, sec := range doc.Toc().Sections {
		name, ok := strings.CutPrefix(sec.Title, "section: ")
		if !ok {
			continue
		}
		rule := SectionRule{}
		if block := firstFence(doc.Source[sec.Start:sec.End]); block != nil {
			if err := strictYAML(block, &rule); err != nil {
				return nil, fmt.Errorf("def %s: INVALID_PARAMS in section %q rule: %v", path, name, err)
			}
		}
		def.Sections = append(def.Sections, DefSection{Name: name, Rule: rule})
	}

	// ^template — heading lines become recognition patterns.
	if tmpl, rerr := doc.Read("^template"); rerr == nil {
		def.Template = templatePatterns(stripFence(tmpl.Content))
	}
	return def, nil
}

// strictYAML decodes src into out with unknown fields rejected.
func strictYAML(src []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && err.Error() != "EOF" {
		return err
	}
	return nil
}

// stripFence removes the opening and closing fence lines of a fenced block
// span (the block index hands back the whole closed fence).
func stripFence(b []byte) []byte {
	lines := bytes.Split(b, []byte("\n"))
	if len(lines) > 0 && bytes.HasPrefix(bytes.TrimSpace(lines[0]), []byte("```")) {
		lines = lines[1:]
	}
	for len(lines) > 0 {
		last := bytes.TrimSpace(lines[len(lines)-1])
		if len(last) == 0 {
			lines = lines[:len(lines)-1]
			continue
		}
		if bytes.HasPrefix(last, []byte("```")) {
			lines = lines[:len(lines)-1]
		}
		break
	}
	return bytes.Join(lines, []byte("\n"))
}

// firstFence returns the inner bytes of the first fenced block in a span, or
// nil when the span has none (a prose-only section declaration).
func firstFence(b []byte) []byte {
	lines := bytes.Split(b, []byte("\n"))
	start := -1
	for i, ln := range lines {
		if bytes.HasPrefix(bytes.TrimSpace(ln), []byte("```")) {
			if start < 0 {
				start = i + 1
				continue
			}
			return bytes.Join(lines[start:i], []byte("\n"))
		}
	}
	return nil
}

// templatePatterns compiles the heading lines of a template body. Fenced
// regions inside the template body are skipped so a template can carry example
// blocks without minting phantom scaffold headings. The template frontmatter
// ("---" fences) carries no headings, so no special-casing is needed.
func templatePatterns(tmpl []byte) []TemplatePattern {
	var out []TemplatePattern
	inFence := false
	for _, ln := range bytes.Split(tmpl, []byte("\n")) {
		trimmed := bytes.TrimSpace(ln)
		if bytes.HasPrefix(trimmed, []byte("```")) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		m := reHeadingLine.FindSubmatch(ln)
		if m == nil {
			continue
		}
		text := string(m[2])
		out = append(out, TemplatePattern{
			Depth:   len(m[1]),
			Text:    text,
			Literal: !reTemplateVar.MatchString(text),
			re:      compileTemplateRe(text),
		})
	}
	return out
}

// compileTemplateRe anchors the literal parts of a template heading and
// wildcards each {{var}} placeholder; compileEntryRe does the same for an
// entry grammar's single-brace {var} placeholders.
func compileTemplateRe(text string) *regexp.Regexp { return compilePattern(text, reTemplateVar) }
func compileEntryRe(text string) *regexp.Regexp    { return compilePattern(text, reEntryVar) }

func compilePattern(text string, vars *regexp.Regexp) *regexp.Regexp {
	var sb strings.Builder
	sb.WriteString("^")
	last := 0
	for _, loc := range vars.FindAllStringIndex(text, -1) {
		sb.WriteString(regexp.QuoteMeta(text[last:loc[0]]))
		sb.WriteString("(.+?)")
		last = loc[1]
	}
	sb.WriteString(regexp.QuoteMeta(text[last:]))
	sb.WriteString("$")
	return regexp.MustCompile(sb.String())
}
