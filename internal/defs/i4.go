// i4.go wires the def validator into the ONE write path as body.Splice's I4
// conformance hook (U7). The severity ladder, verbatim from the plan:
//
//	error   → refuse (never forceable)
//	warning → refuse unless Force() — forced rule ids are journaled and censused (R-force)
//	repair  → autofill (v1: stamp:close timestamps on a terminal status; nothing else)
//
// The hook judges the WRITE, not the file: findings are DELTA-scored against the
// pre-write document, so a record that already carries legacy findings stays
// writable (decision 7's "left untouched" extends to the write path) and only a
// write that INTRODUCES a violation is refused. Two carve-outs from the delta:
//
//   - def/legacy-entry never blocks: applying the legacy MARK is the sanctioned
//     remedy for an off-grammar entry — blocking it would block `md def fix`.
//   - a kind with NO def anywhere (ErrNoDef) passes: an undeclared kind is not a
//     record contract. A def that EXISTS but fails to load is different — that is
//     rot in the schema layer, surfaced as a warning-rung refusal (forceable,
//     audited) rather than a silent skip or a fleet-wide hard outage.
//
// Registration is by init(): any binary linking this package (cmd/md via the def
// verbs, the pipe handler, daemon faces) arms I4 automatically; internal/body's
// own tests run with the hook unset and exercise I0–I3 pure.
package defs

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/body"
	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/types"
)

func init() { body.SetConformance(SpliceConformance) }

// nonBlockingWarns are warning rules excluded from the refuse-unless-force rung
// (see the file doc). def/census findings are info-severity and never reach here.
var nonBlockingWarns = map[string]bool{"def/legacy-entry": true}

// SpliceConformance is the I4 hook (body.ConformanceFunc). It never mutates
// anything itself — repairs are returned as edits for Splice to apply through
// its own plan/reparse machinery.
func SpliceConformance(req body.ConformanceRequest) body.ConformanceResult {
	kind, preset := recordKind(req.Next)
	if kind == "" {
		// Not a typed record: only the substrate law applies (nested frontmatter
		// is an error always, from any writer).
		if refuse := nestedDelta(req); refuse != nil {
			return body.ConformanceResult{Refuse: refuse}
		}
		return body.ConformanceResult{}
	}

	def, rerr := Resolve(kind, preset, DiscoverLayers(req.Target))
	if errors.Is(rerr, ErrNoDef) {
		if refuse := nestedDelta(req); refuse != nil {
			return body.ConformanceResult{Refuse: refuse}
		}
		return body.ConformanceResult{}
	}
	if rerr != nil {
		if refuse := nestedDelta(req); refuse != nil {
			return body.ConformanceResult{Refuse: refuse}
		}
		if req.Force {
			return body.ConformanceResult{Forced: []string{"def/malformed"}}
		}
		return body.ConformanceResult{Refuse: &body.Error{
			Code:    "E_CONFORMANCE",
			Message: "the " + kind + " def failed to load, so this write cannot be validated: " + rerr.Error(),
			Remedy:  "fix the def file (fail closed), or re-submit with force to override — forced writes are journaled and surface in the def census",
			Context: map[string]string{"kind": kind, "rule": "def/malformed"},
		}}
	}

	repairs := planCloseStamps(req.Next, def)
	newErrs, newWarns := deltaFindings(Check(req.Prev, def).Findings, Check(req.Next, def).Findings)
	if len(repairs) > 0 {
		// The repair resolves exactly the "terminal without closed_at" direction of
		// the biconditional; drop it from the error set so the repair, not a
		// refusal, handles it. Every other error still refuses below.
		newErrs = dropRule(newErrs, "def/biconditional")
	}
	if len(newErrs) > 0 {
		return body.ConformanceResult{Refuse: conformanceRefusal(kind, req.Target, newErrs,
			"the write was refused; the file is unchanged. Fix the violation (`md def check "+req.Target+"` explains the def) — errors are never forceable")}
	}
	if len(newWarns) > 0 && !req.Force {
		return body.ConformanceResult{Refuse: conformanceRefusal(kind, req.Target, newWarns,
			"fix the content, or re-submit with force to override — forced warnings are journaled and surface in the def census (R-force)")}
	}
	res := body.ConformanceResult{Repairs: repairs}
	if req.Force {
		res.Forced = ruleIDs(newWarns)
	}
	return res
}

// recordKind reads the candidate's type/preset. Unreadable frontmatter yields ""
// — the def layer then has nothing to resolve by; the frontmatter findings
// themselves are delta-scored via nestedDelta/Check like any other.
func recordKind(doc *body.Document) (kind, preset string) {
	fm, err := frontmatter.ParseBytes(doc.Bytes())
	if err != nil || fm == nil {
		return "", ""
	}
	return fm.StringField("type"), fm.StringField("preset")
}

// nestedDelta refuses a write that INTRODUCES a substrate-law violation (nested
// frontmatter) on a document with no def to validate against. Never forceable.
func nestedDelta(req body.ConformanceRequest) *body.Error {
	newErrs, _ := deltaFindings(ScanNested(req.Prev), ScanNested(req.Next))
	if len(newErrs) == 0 {
		return nil
	}
	return conformanceRefusal("", req.Target, newErrs,
		"the write was refused; the file is unchanged. Flatten the value (nested frontmatter is an error always, from any writer) — never forceable")
}

// planCloseStamps is the v1 repair rung: when the candidate sits at a terminal
// status and a stamp:close property (canonically closed_at) is empty, autofill it
// with now — the one derived fact whose source is authoritative (the transition
// is in front of us). Everything else (status, owner, verdict, answer — domain
// facts) is refused, never invented: "data corruption with a friendly face".
// stamp:touch autofill is DEFERRED beyond v1 (it would churn every write).
func planCloseStamps(doc *body.Document, def *Def) []body.Edit {
	spec, ok := def.Props["status"]
	if !ok || len(spec.Terminal) == 0 {
		return nil
	}
	fm, err := frontmatter.ParseBytes(doc.Bytes())
	if err != nil || fm == nil {
		return nil
	}
	status, _ := fm.Meta["status"].(string)
	if !contains(spec.Terminal, status) {
		return nil
	}
	var keys []string
	for key, p := range def.Props {
		if p.Stamp == "close" && fm.Meta[key] == nil {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	now := time.Now().Format("2006-01-02T15:04:05")
	edits := make([]body.Edit, 0, len(keys))
	for _, k := range keys {
		edits = append(edits, body.Edit{Op: body.OpSetProperty, Target: k, New: now})
	}
	return edits
}

// deltaFindings partitions the findings present in next but NOT in prev into
// error- and warning-rung sets. Keying is (rule, message) — line numbers shift
// under a splice and must not make an old finding look new. Info findings
// (census) never block. Warnings on the non-blocking list are dropped.
func deltaFindings(prev, next []types.Finding) (errs, warns []types.Finding) {
	seen := map[string]bool{}
	for _, f := range prev {
		seen[f.RuleID+"|"+f.Message] = true
	}
	for _, f := range next {
		if seen[f.RuleID+"|"+f.Message] {
			continue
		}
		switch f.Severity {
		case "error":
			errs = append(errs, f)
		case "warn":
			if !nonBlockingWarns[f.RuleID] {
				warns = append(warns, f)
			}
		}
	}
	return errs, warns
}

// conformanceRefusal renders a refusal naming the first violation (and how many
// more), the def kind, and the executable remedy.
func conformanceRefusal(kind, target string, findings []types.Finding, remedy string) *body.Error {
	msg := findings[0].Message
	if n := len(findings); n > 1 {
		msg += fmt.Sprintf(" (and %d more)", n-1)
	}
	subject := "the record's def"
	if kind != "" {
		subject = "the " + kind + " def"
	}
	return &body.Error{
		Code:    "E_CONFORMANCE",
		Message: "write violates " + subject + ": " + msg,
		Remedy:  remedy,
		Context: map[string]string{"kind": kind, "target": target, "rules": strings.Join(ruleIDs(findings), ",")},
	}
}

func ruleIDs(findings []types.Finding) []string {
	var out []string
	for _, f := range findings {
		out = appendDistinctStr(out, f.RuleID)
	}
	return out
}

func dropRule(findings []types.Finding, rule string) []types.Finding {
	out := findings[:0]
	for _, f := range findings {
		if f.RuleID != rule {
			out = append(out, f)
		}
	}
	return out
}

func appendDistinctStr(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}
