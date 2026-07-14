package attest

import (
	"errors"
	"regexp"
	"sort"
	"strings"
)

// written carries the values a write would record.
type written struct {
	target    string
	sums      []string
	proc      string
	appliedAt string
	verdict   string // "" = leave the recorded verdict untouched (R5: cached-verdict refs are legal)
	hashes    []string
}

// edit is one line-range splice: delete del lines at line, insert insert.
type edit struct {
	line   int
	del    int
	insert []string
}

// fmReceiptRe matches the frontmatter receipt key line for the null→pointer flip.
var fmReceiptRe = regexp.MustCompile(`^receipt:(\s|$)`)

// buildEdit constructs the new page bytes for the classified case, surgically:
// only the lines of fields that change are touched — key order and unknown
// fields survive byte-for-byte (the golden-file round-trip contract). It also
// returns the old→new value maps for the step-6 report.
func buildEdit(p *pageState, cls *classified, w written) ([]byte, map[string]any, map[string]any, error) {
	oldVals := map[string]any{}
	newVals := map[string]any{}

	// Chain hashes (all cases with changed inputs).
	edits := chainHashEdits(p, cls.writeInputs, w.hashes)
	if n := len(cls.writeInputs); n > 0 {
		oldVals["inputs_rehashed"] = n
		newVals["inputs_rehashed"] = n
	}

	// Receipt fields (pointed pages only; owned pages never reach here with a
	// receipt case — classify hands them inputs-only).
	setKeys := receiptSets(p, cls, w, oldVals, newVals)
	if cls.name == CaseFirst {
		// New block appended at the page tail (machine regions contiguous at
		// the tail, per the schema's suggested order) + the one-time
		// frontmatter flip — the machinery's ONLY frontmatter write.
		flip, err := flipEdit(p)
		if err != nil {
			return nil, nil, nil, err
		}
		edits = append(edits, flip)
		return applyEdits(p, edits, receiptBlockLines(setKeys)), oldVals, newVals, nil
	}
	if len(setKeys) > 0 {
		var inserts []string
		for _, key := range managedReceiptKeys {
			val, ok := setKeys[key]
			if !ok {
				continue
			}
			line := key + ": " + val
			if kl, exists := p.rec.keyLine[key]; exists {
				edits = append(edits, edit{line: kl, del: p.rec.keyEnd[key] - kl, insert: []string{line}})
			} else {
				inserts = append(inserts, line)
			}
		}
		if len(inserts) > 0 {
			edits = append(edits, edit{line: p.receipt.close, del: 0, insert: inserts})
		}
	}
	return applyEdits(p, edits, nil), oldVals, newVals, nil
}

// chainHashEdits rewrites the machine `hash:` line of each named input item —
// surgical, one line per item, so unrelated inputs and any claim prose survive
// byte-for-byte. An item whose `hash:` line is absent gets one inserted as a
// `ref:` sibling directly below it (the pre-first-attest shape) — never at the
// block tail, where the canonical trailing `hash-algo` scalar sits. Shared by the
// four-case write path and the bulk cosmetic sweep.
func chainHashEdits(p *pageState, writeInputs []int, hashes []string) []edit {
	var edits []edit
	for _, i := range writeInputs {
		item := p.items[i]
		val := item.hashIndent + "hash: " + yamlQuote(hashes[i])
		if item.hashLine >= 0 {
			edits = append(edits, edit{line: item.hashLine, del: 1, insert: []string{val}})
		} else {
			edits = append(edits, edit{line: item.appendLine, del: 0, insert: []string{val}})
		}
	}
	return edits
}

// buildBulkEdit constructs the bytes for a bulk re-attest (§6.1): the drifted
// chain hashes are rewritten and — on a pointed page — applied_at is stamped
// (owned pages write input hashes only, P20). No receipt field moves: a cosmetic
// sweep records that the channel was re-checked, never that the tree or the
// procedure did. Returns the old→new report maps.
func buildBulkEdit(p *pageState, changed []int, hashes []string, appliedAt string) ([]byte, map[string]any, map[string]any) {
	oldVals := map[string]any{"inputs_rehashed": len(changed)}
	newVals := map[string]any{"inputs_rehashed": len(changed)}
	edits := chainHashEdits(p, changed, hashes)
	if p.pointed && p.receipt != nil {
		if kl, ok := p.rec.keyLine[keyAppliedAt]; ok {
			edits = append(edits, edit{line: kl, del: p.rec.keyEnd[keyAppliedAt] - kl, insert: []string{keyAppliedAt + ": " + appliedAt}})
		} else {
			edits = append(edits, edit{line: p.receipt.close, del: 0, insert: []string{keyAppliedAt + ": " + appliedAt}})
		}
		oldVals[keyAppliedAt] = p.rec.AppliedAt
		newVals[keyAppliedAt] = appliedAt
	}
	return applyEdits(p, edits, nil), oldVals, newVals
}

// receiptSets decides which managed keys the case writes, formatting each
// value canonically. A key whose recorded value already equals the new value
// is NOT rewritten (byte-minimal writes; case-2a's byte-stability contract is
// the strong form of the same rule).
func receiptSets(p *pageState, cls *classified, w written, oldVals, newVals map[string]any) map[string]string {
	set := map[string]string{}
	if !p.pointed {
		return set
	}
	record := func(key, newRaw, oldRaw string) {
		oldVals[key] = oldRaw
		newVals[key] = newRaw
	}
	switch cls.name {
	case CaseFirst:
		set[keyCommit] = w.target
		set[keyChecksum] = checksumValue(w.sums)
		set[keyAppliedAt] = w.appliedAt
		if w.verdict != "" {
			set[keyVerdict] = yamlQuote(w.verdict)
			newVals[keyVerdict] = w.verdict
		}
		set[keyProcedureHash] = yamlQuote(w.proc)
		newVals[keyCommit] = w.target
		newVals[keyChecksum] = w.sums
		newVals[keyProcedureHash] = w.proc
	case CaseTree:
		if p.rec.Commit != w.target {
			set[keyCommit] = w.target
			record(keyCommit, w.target, p.rec.Commit)
		}
		set[keyChecksum] = checksumValue(w.sums)
		oldVals[keyChecksum] = p.rec.Checksums
		newVals[keyChecksum] = w.sums
		set[keyAppliedAt] = w.appliedAt
		if p.rec.ProcedureHash != w.proc {
			set[keyProcedureHash] = yamlQuote(w.proc)
			record(keyProcedureHash, w.proc, p.rec.ProcedureHash)
		}
		if w.verdict != "" && w.verdict != p.rec.Verdict {
			set[keyVerdict] = yamlQuote(w.verdict)
			record(keyVerdict, w.verdict, p.rec.Verdict)
		}
	case CaseProcedure:
		// 2b: the receipt moves — procedure-hash, verdict, applied_at; commit
		// and checksum stay byte-stable (the tree did not move).
		set[keyProcedureHash] = yamlQuote(w.proc)
		record(keyProcedureHash, w.proc, p.rec.ProcedureHash)
		set[keyAppliedAt] = w.appliedAt
		if w.verdict != "" && w.verdict != p.rec.Verdict {
			set[keyVerdict] = yamlQuote(w.verdict)
			record(keyVerdict, w.verdict, p.rec.Verdict)
		}
	case CaseInputsOnly:
		// 2a: chain hashes + the verdict pointer refresh only —
		// commit/checksum/applied_at/procedure-hash stay byte-stable (a
		// pointer refresh is evaluation-evidence bookkeeping, not a receipt
		// move).
		if w.verdict != "" && w.verdict != p.rec.Verdict {
			set[keyVerdict] = yamlQuote(w.verdict)
			record(keyVerdict, w.verdict, p.rec.Verdict)
		}
	}
	return set
}

// flipEdit locates the frontmatter `receipt:` line for the null→pointer flip.
func flipEdit(p *pageState) (edit, error) {
	for i := 0; i < p.fmEnd && i < len(p.lines); i++ {
		if fmReceiptRe.MatchString(p.lines[i]) {
			return edit{line: i, del: 1, insert: []string{"receipt: '[[#^receipt]]'"}}, nil
		}
	}
	return edit{}, errors.New("frontmatter receipt key not found for the null→pointer flip")
}

// receiptBlockLines renders a fresh ^receipt unit for the first attestation.
func receiptBlockLines(setKeys map[string]string) []string {
	out := []string{"", "## Receipt", "", "```yaml"}
	for _, key := range managedReceiptKeys {
		if val, ok := setKeys[key]; ok {
			out = append(out, key+": "+val)
		}
	}
	return append(out, "```", "^receipt")
}

// applyEdits splices the edits (descending, so indexes stay valid), optionally
// appends a new tail block, and reassembles the file byte-exactly (\n join —
// untouched lines are never reflowed).
func applyEdits(p *pageState, edits []edit, tailBlock []string) []byte {
	lines := append([]string(nil), p.lines...)
	sort.Slice(edits, func(i, j int) bool { return edits[i].line > edits[j].line })
	for _, e := range edits {
		var next []string
		next = append(next, lines[:e.line]...)
		next = append(next, e.insert...)
		next = append(next, lines[e.line+e.del:]...)
		lines = next
	}
	if tailBlock != nil {
		// Normalize the tail to one trailing newline, then append the unit.
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, tailBlock...)
		lines = append(lines, "") // final newline
	}
	return []byte(strings.Join(lines, "\n"))
}

// yamlQuote single-quotes a scalar for a machine-written YAML line.
func yamlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// checksumValue renders the per-location checksums: a bare scalar for one
// location, a flow sequence for several (paired by index with frontmatter
// location, the pin-era convention carried forward).
func checksumValue(sums []string) string {
	if len(sums) == 1 {
		return sums[0]
	}
	return "[" + strings.Join(sums, ", ") + "]"
}
