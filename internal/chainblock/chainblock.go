// Package chainblock is the ONE structural parser of a receipt-era `^inputs`
// machine block (SCHEMA "Effect pages — the receipt schema"). It exists so the
// fact extractor (read/freshness) and the attest writer (surgical hash rewrites)
// derive the edge set of a chain block from the SAME parse — a line-regex reader
// and the YAML writer can never disagree about which entries are edges.
//
// The class this closes: a first-match line scan that trims each line before
// testing `- ` treats a dash-bulleted line INSIDE a `claim: |` block scalar as a
// sequence entry (a phantom edge; the third occurrence of the
// line-regex-vs-structural class after B1b's off-by-one and attest's CORR-1).
// The real YAML parser attributes block-scalar bytes to their key's value, so a
// `- item` under `claim: |` is prose, never an edge.
//
// The `^inputs` block is a YAML sequence OPTIONALLY followed by top-level scalars
// (`hash-algo: v1`) — a shape a single yaml decode rejects ("did not find
// expected '-' indicator"). Parse splits the leading sequence from the trailing
// scalars on the first column-0 non-dash line (block-scalar content is always
// indented, so such a line can only be block metadata), then decodes ONLY the
// sequence region structurally.
package chainblock

import (
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// Item is one `^inputs` sequence entry parsed from the block YAML. RefLine and
// HashLine are 1-indexed relative to the block CONTENT (line 1 = the first line
// below the opening fence); a caller adds its own file-line base. HashCol is the
// 0-indexed byte column of the `hash:` key — the surgical-rewrite coordinate the
// writer binds from node position, never from a raw-line regex.
type Item struct {
	Ref      string // raw ref value as authored, yaml-unquoted ("[[page#Section]]"); "" when the entry has no ref: key
	Hash     string // recorded hash, yaml-unquoted; "" for null / ~ / absent / empty
	HasHash  bool   // a hash: key was present (distinguishes an absent hash from a null one)
	RefLine  int    // content line of the ref: key (0 when the entry has no ref: key)
	HashLine int    // content line of the hash: key (0 when absent)
	HashCol  int    // 0-indexed byte column of the hash: key (-1 when absent)
}

// Result is a parsed `^inputs` block: its ordered edge items plus the trailing
// `hash-algo` contract-version scalar ("" when absent).
type Result struct {
	Items    []Item
	HashAlgo string

	// StrayMeta is every column-0 line that is neither a sequence entry, a
	// comment, nor the recognized `hash-algo` scalar. The tolerant read path
	// recovers edges ACROSS such lines (StrayMeta stays advisory); the attest
	// writer fails closed on a non-empty StrayMeta — a surgical hash rewrite must
	// not step around structure it does not model. Each element is the trimmed
	// line content, in file order.
	StrayMeta []string
}

// Parse decodes an `^inputs` block's inner content — the text strictly BETWEEN
// its fences, with fences and the `^id` marker already removed. A non-empty
// problem names a fail-closed case (the sequence region is not a YAML sequence of
// mappings, or an item declares hash twice): the writer must refuse to write on a
// non-empty problem; the read path may use the returned Items regardless (it is
// tolerant, and Parse never emits a phantom edge — a refless or malformed entry
// is reported, never invented). Items carry every well-formed entry parsed before
// the first fail-closed condition.
func Parse(content string) (Result, string) {
	lines := strings.Split(content, "\n")

	// A `^inputs` block is a YAML sequence with column-0 metadata scalars
	// (`hash-algo: v1`) trailing (or, degenerately, interleaved). Everything a
	// sequence item owns is either its `- ` line or indented under it, so ANY
	// column-0 non-dash, non-comment line is block metadata, never edge content.
	// Blank each such line OUT of the sequence text — capturing hash-algo — while
	// keeping its line slot, so yaml.Node line numbers stay aligned with the block
	// content and every edge keeps its true file line. This recovers all genuine
	// edges even when a stray column-0 line sits between them (never a silent
	// truncation), while a block-scalar's indented dash lines stay prose (the
	// class this parser closes). A block with NO sequence entry is left whole for
	// the decoder to fail closed on — a mapping is not a sequence, never a silent
	// empty parse.
	hasEntry := false
	for _, ln := range lines {
		if t := strings.TrimSpace(ln); isColumn0(ln) && (t == "-" || strings.HasPrefix(t, "- ")) {
			hasEntry = true
			break
		}
	}

	var res Result
	seqText := content
	if hasEntry {
		seq := make([]string, len(lines))
		for i, ln := range lines {
			t := strings.TrimSpace(ln)
			if isColumn0(ln) && t != "" && !strings.HasPrefix(t, "#") && t != "-" && !strings.HasPrefix(t, "- ") {
				if k, v, ok := splitScalar(t); ok && k == "hash-algo" {
					res.HashAlgo = v
				} else {
					res.StrayMeta = append(res.StrayMeta, t) // unmodeled column-0 line: the writer's fail-closed signal
				}
				continue // metadata: leave seq[i] blank, preserving the line slot
			}
			seq[i] = ln
		}
		seqText = strings.Join(seq, "\n")
	}

	if strings.TrimSpace(seqText) == "" {
		return res, "" // empty block — the caller reports "inputs is empty"
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(seqText), &root); err != nil {
		return res, "inputs block is not a YAML sequence: " + err.Error()
	}
	if len(root.Content) == 0 {
		return res, ""
	}
	seq := root.Content[0]
	if seq.Kind != yaml.SequenceNode {
		return res, "inputs block is not a YAML sequence"
	}

	// yaml.Node lines are 1-indexed relative to seqText, whose line 1 is the
	// block content's first line (seqText is a prefix of the content) — so a node
	// line IS a content line, no offset.
	for n, itemNode := range seq.Content {
		if itemNode.Kind != yaml.MappingNode {
			return res, fmt.Sprintf("inputs item %d is not a mapping", n+1)
		}
		item := Item{HashLine: 0, HashCol: -1}
		for k := 0; k+1 < len(itemNode.Content); k += 2 {
			key, val := itemNode.Content[k], itemNode.Content[k+1]
			switch key.Value {
			case "ref":
				item.Ref = strings.TrimSpace(val.Value)
				item.RefLine = key.Line
			case "hash":
				if item.HasHash {
					return res, fmt.Sprintf("inputs item %d declares hash twice", n+1)
				}
				item.HasHash = true
				item.HashLine = key.Line
				item.HashCol = key.Column - 1
				if val.Tag != "!!null" && val.Value != "" {
					item.Hash = strings.TrimSpace(val.Value)
				}
			}
		}
		res.Items = append(res.Items, item)
	}
	return res, ""
}

// isColumn0 reports a non-blank line whose first byte is not indentation.
func isColumn0(ln string) bool {
	return ln != "" && ln[0] != ' ' && ln[0] != '\t'
}

// splitScalar splits a "key: value" line, stripping an unquoted trailing
// `# comment` and surrounding quotes from the value. ok=false when no colon.
func splitScalar(line string) (key, value string, ok bool) {
	i := strings.Index(line, ":")
	if i < 1 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:i])
	value = strings.TrimSpace(line[i+1:])
	if !strings.HasPrefix(value, "'") && !strings.HasPrefix(value, "\"") {
		if j := strings.Index(value, " #"); j != -1 {
			value = strings.TrimSpace(value[:j])
		}
	}
	value = strings.Trim(value, "'\"")
	return key, value, true
}
