package attest

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
	yaml "go.yaml.in/yaml/v3"
)

// Receipt-block keys the machinery manages (contract 4f3cbef §4.1). Everything
// else in the block is unknown-field territory: preserved byte-for-byte.
const (
	keyCommit        = "commit"
	keyChecksum      = "checksum"
	keyAppliedAt     = "applied_at"
	keyVerdict       = "verdict"
	keyProcedureHash = "procedure-hash"
)

var managedReceiptKeys = []string{keyCommit, keyChecksum, keyAppliedAt, keyVerdict, keyProcedureHash}

// pageState is the parsed working state of one effect page — built once from
// the scan-snapshot bytes; those bytes are also the CAS base (P6).
type pageState struct {
	rel   string
	raw   []byte
	lines []string // raw split on \n; edits reassemble with \n join (byte-exact)

	fm    map[string]any
	tags  []string
	fmEnd int // line index of the closing frontmatter "---"

	pointed     bool // frontmatter carries the receipt key (explicit-null rule)
	receiptNull bool // receipt: null — pointed, never applied
	repo        string
	branch      string // page-level branch (transitional tolerance; source page is the home)
	locations   []string

	inputs  fencedBlock // the ^inputs data block (required)
	items   []inputItem
	receipt *fencedBlock // the ^receipt data block (nil until first attestation)
	rec     receiptRec
}

// inputItem is one chain entry: the parsed ref/hash plus the line coordinates
// its machine-written hash occupies (file-absolute), for surgical rewrites.
type inputItem struct {
	Ref  string
	Hash string // recorded hash; "" for null/absent (declared, never attested)

	start, end int // file line range of the item [start, end)
	hashLine   int // file line of its hash: entry; -1 when absent
	hashIndent string
}

// receiptRec is the parsed ^receipt block plus per-key line coordinates.
type receiptRec struct {
	Commit        string
	Checksums     []string
	AppliedAt     string
	Verdict       string
	ProcedureHash string

	keyLine map[string]int // managed key → file line of its entry; absent = missing
	keyEnd  map[string]int // exclusive end of the key's value lines (multi-line lists)
}

// fencedBlock is a located ^id-addressed fenced block: file line indexes of
// its fence delimiters (content = lines (open, close) exclusive).
type fencedBlock struct {
	open, close int
}

var (
	fenceDelimRe = regexp.MustCompile("^(`{3,}|~{3,})(.*)$")
	seqEntryRe   = regexp.MustCompile(`^\s+- `)
)

// parsePage builds the pageState. A non-empty problem is a page-level finding
// (fail closed, no write); skip distinguishes shape-screen exclusions (legacy
// pin pages: counted, named, skipped — never attested, never failed).
func parsePage(rel string, raw []byte) (p *pageState, problem string, skip string) {
	doc, err := frontmatter.ParseBytes(raw)
	if err != nil {
		return nil, "unparseable frontmatter: " + err.Error(), ""
	}
	if doc == nil || len(doc.Meta) == 0 {
		return nil, "no frontmatter — not an effect page", ""
	}
	p = &pageState{
		rel:   rel,
		raw:   raw,
		lines: strings.Split(string(raw), "\n"),
		fm:    doc.Meta,
	}
	p.fmEnd = doc.BodyOffset - 2 // BodyOffset is the 1-indexed first body line; closing --- sits above it
	p.tags = extractTags(doc.Meta)
	if !hasTag(p.tags, "type/effect") {
		return nil, "not a type/effect page", ""
	}

	// Shape screen (P19 / U-B4b): a frontmatter commit pin marks the legacy or
	// transitional shape — counted, named, skipped. md attest writes only
	// receipt-shape pages (inputs present, no frontmatter pin).
	if fmStr(p.fm, "commit") != "" {
		if _, hasInputs := p.fm["inputs"]; hasInputs {
			return nil, "", "transitional shape (frontmatter commit pin AND inputs) — finish the migration before attesting"
		}
		return nil, "", "legacy shape (frontmatter commit pin) — migrate to receipt shape before attesting"
	}

	rv, hasReceipt := p.fm["receipt"]
	p.pointed = hasReceipt
	p.receiptNull = hasReceipt && rv == nil
	p.repo = fmStr(p.fm, "repo")
	p.branch = fmStr(p.fm, "branch")
	p.locations = fmList(p.fm, "location")

	// The chain block: inputs MUST be non-empty (§5.3 — a claim from nowhere is
	// void); the frontmatter pointer and the ^inputs block must both exist.
	if _, ok := p.fm["inputs"]; !ok {
		return nil, "no inputs — a rootless effect cannot be attested (declare the chain in a ^inputs block)", ""
	}
	blk, found, err := locateFencedBlock(p.lines, p.fmEnd+1, "inputs")
	if err != nil {
		return nil, "inputs block: " + err.Error(), ""
	}
	if !found {
		return nil, "frontmatter declares inputs but no ^inputs fenced block exists on the page", ""
	}
	p.inputs = blk
	if problem := p.parseInputs(); problem != "" {
		return nil, problem, ""
	}
	if len(p.items) == 0 {
		return nil, "inputs block is empty — a rootless effect cannot be attested", ""
	}

	// The receipt block (pointed pages, after first attestation).
	rblk, rfound, err := locateFencedBlock(p.lines, p.fmEnd+1, "receipt")
	if err != nil {
		return nil, "receipt block: " + err.Error(), ""
	}
	if rfound {
		p.receipt = &rblk
		if problem := p.parseReceipt(); problem != "" {
			return nil, problem, ""
		}
	}

	// Consistency gates (strict writer):
	if p.pointed && !p.receiptNull && p.receipt == nil {
		return nil, "receipt frontmatter points at [[#^receipt]] but the block is missing — dangling receipt pointer", ""
	}
	if p.pointed && p.receiptNull && p.receipt != nil {
		return nil, "receipt: null but a ^receipt block exists — inconsistent birth state", ""
	}
	if !p.pointed && p.receipt != nil {
		return nil, "owned effect (no receipt key) carries a ^receipt block — owned pages are receipt-free by definition (§2.3)", ""
	}
	return p, "", ""
}

// locateFencedBlock finds the fenced block addressed by a standalone ^id line
// (outside any fence), mirroring run.FindBlock's addressing rules but keeping
// file-absolute line coordinates for surgical edits. found=false when the id
// is absent; err on a duplicate id or a marker not anchored to a fence.
func locateFencedBlock(lines []string, bodyStart int, id string) (fencedBlock, bool, error) {
	marker := regexp.MustCompile(`^\^` + regexp.QuoteMeta(id) + `\s*$`)
	fenceChar := byte(0)
	fenceLen := 0
	openIdx := -1
	lastClosed := fencedBlock{-1, -1}
	var hits []fencedBlock
	dup := 0
	for i := bodyStart; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\r")
		if fenceChar != 0 {
			if m := fenceDelimRe.FindStringSubmatch(line); m != nil &&
				m[1][0] == fenceChar && len(m[1]) >= fenceLen && strings.TrimSpace(m[2]) == "" {
				lastClosed = fencedBlock{openIdx, i}
				fenceChar = 0
			}
			continue
		}
		if m := fenceDelimRe.FindStringSubmatch(line); m != nil {
			fenceChar = m[1][0]
			fenceLen = len(m[1])
			openIdx = i
			continue
		}
		if marker.MatchString(line) {
			dup++
			// The block above the marker (skipping blanks) must be the last
			// closed fence, ending directly above.
			j := i - 1
			for j >= 0 && strings.TrimSpace(lines[j]) == "" {
				j--
			}
			if lastClosed.close != j {
				return fencedBlock{}, false, fmt.Errorf("^%s marker is not anchored to a fenced block — the data block must be a fenced YAML block directly above its marker", id)
			}
			hits = append(hits, lastClosed)
		}
	}
	switch dup {
	case 0:
		return fencedBlock{}, false, nil
	case 1:
		return hits[0], true, nil
	default:
		return fencedBlock{}, false, fmt.Errorf("^%s is declared %d times — block IDs must be unique", id, dup)
	}
}

// parseInputs decodes the ^inputs block (ordered YAML sequence of
// {ref, claim?, hash?}) and binds each item's machine `hash:` line
// STRUCTURALLY — from the parsed YAML mapping-key node's own line/column, never
// by a raw-line regex. A `hash:`-shaped line inside a `claim: |` block-scalar is
// human prose, not the key: the parser distinguishes them, a first-match regex
// cannot (CORR-1). Fail closed on anything it cannot map — a hash written to the
// wrong item, or spliced into claim text, is worse than an error.
func (p *pageState) parseInputs() string {
	content := blockContent(p.lines, p.inputs)
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return "inputs block is not a YAML sequence: " + err.Error()
	}
	if len(root.Content) == 0 {
		return "" // empty block — the caller reports "inputs block is empty"
	}
	seq := root.Content[0]
	if seq.Kind != yaml.SequenceNode {
		return "inputs block is not a YAML sequence — refusing to write hashes"
	}

	// yaml.Node lines are 1-indexed relative to the unmarshaled content, whose
	// line 1 is the line just below the opening fence; file-absolute line =
	// p.inputs.open + node.Line.
	base := p.inputs.open
	for n, itemNode := range seq.Content {
		if itemNode.Kind != yaml.MappingNode {
			return fmt.Sprintf("inputs item %d is not a mapping — refusing to write hashes", n+1)
		}
		item := inputItem{start: base + itemNode.Line, end: p.inputs.close, hashLine: -1, hashIndent: "  "}
		if n+1 < len(seq.Content) {
			item.end = base + seq.Content[n+1].Line
		}
		var ref string
		sawHash := false
		for k := 0; k+1 < len(itemNode.Content); k += 2 {
			key, val := itemNode.Content[k], itemNode.Content[k+1]
			switch key.Value {
			case "ref":
				ref = strings.TrimSpace(val.Value)
			case "hash":
				if sawHash {
					return fmt.Sprintf("inputs item %d declares hash twice — ambiguous, refusing to write", n+1)
				}
				sawHash = true
				line := base + key.Line
				col := key.Column - 1
				// Belt: the located line must actually carry the mapping key at
				// the node's column — fail closed when structure and bytes
				// disagree rather than splice a hash into an unconfirmed line.
				if line >= len(p.lines) || col < 0 || col > len(p.lines[line]) ||
					!strings.HasPrefix(p.lines[line][col:], "hash:") {
					return fmt.Sprintf("inputs item %d: hash key node does not resolve to its line — refusing to write", n+1)
				}
				item.hashLine = line
				item.hashIndent = p.lines[line][:col]
				if val.Tag != "!!null" && val.Value != "" {
					item.Hash = strings.TrimSpace(val.Value)
				}
			}
		}
		if ref == "" {
			return fmt.Sprintf("inputs item %d has no ref", n+1)
		}
		item.Ref = ref
		p.items = append(p.items, item)
	}
	return ""
}

// parseReceipt decodes the ^receipt block and maps every managed key to its
// entry lines. Unknown keys are never touched (tolerant reader; the golden-
// file round-trip in the write-path suite proves it).
func (p *pageState) parseReceipt() string {
	content := blockContent(p.lines, *p.receipt)
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		return "receipt block is not a YAML mapping: " + err.Error()
	}
	p.rec = receiptRec{keyLine: map[string]int{}, keyEnd: map[string]int{}}
	p.rec.Commit = anyStr(parsed[keyCommit])
	p.rec.Checksums = anyList(parsed[keyChecksum])
	p.rec.AppliedAt = anyStr(parsed[keyAppliedAt])
	p.rec.Verdict = anyStr(parsed[keyVerdict])
	p.rec.ProcedureHash = anyStr(parsed[keyProcedureHash])

	for _, key := range managedReceiptKeys {
		re := regexp.MustCompile(`^` + regexp.QuoteMeta(key) + `:(\s|$)`)
		for i := p.receipt.open + 1; i < p.receipt.close; i++ {
			if !re.MatchString(p.lines[i]) {
				continue
			}
			p.rec.keyLine[key] = i
			end := i + 1
			for end < p.receipt.close && seqEntryRe.MatchString(p.lines[end]) {
				end++ // a block-sequence value (multi-line checksum list)
			}
			p.rec.keyEnd[key] = end
			break
		}
	}
	return ""
}

// --- small extractors ---

func blockContent(lines []string, b fencedBlock) string {
	return strings.Join(lines[b.open+1:b.close], "\n")
}

func fmStr(fm map[string]any, key string) string {
	if v, ok := fm[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func fmList(fm map[string]any, key string) []string {
	switch v := fm[key].(type) {
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return []string{s}
		}
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}

func anyStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func anyList(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		var out []string
		for _, item := range t {
			if s := anyStr(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := anyStr(v); s != "" {
			return []string{s}
		}
	}
	return nil
}

func extractTags(fm map[string]any) []string {
	raw, ok := fm["tags"].([]any)
	if !ok {
		return nil
	}
	var tags []string
	for _, t := range raw {
		if s, ok := t.(string); ok {
			tags = append(tags, s)
		}
	}
	return tags
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}
