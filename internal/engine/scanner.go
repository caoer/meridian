package engine

import (
	"io/fs"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
)

// inlineSuppressHTMLRe matches `<!-- md-disable-next-line rule-a, rule-b -->`.
var inlineSuppressHTMLRe = regexp.MustCompile(`<!--\s*md-disable-next-line\s+([^>]+?)\s*-->`)

// inlineSuppressObsidianRe matches `%% md-disable-next-line rule-a, rule-b %%`.
var inlineSuppressObsidianRe = regexp.MustCompile(`%%\s*md-disable-next-line\s+([^%]+?)\s*%%`)

// mdIgnoreHTMLRe matches `<!-- md:ignore [rule-a, rule-b] -->`.
// Optional rule list: when absent, suppresses all rules (wildcard).
var mdIgnoreHTMLRe = regexp.MustCompile(`<!--\s*md:ignore(?:\s+([^>]+?))?\s*-->`)

// mdIgnoreObsidianRe matches `%% md:ignore [rule-a, rule-b] %%`.
var mdIgnoreObsidianRe = regexp.MustCompile(`%%\s*md:ignore(?:\s+([^%]+?))?\s*%%`)

// mdIgnoreFileHTMLRe matches `<!-- md:ignore-file [rule-a, rule-b] -->`.
var mdIgnoreFileHTMLRe = regexp.MustCompile(`<!--\s*md:ignore-file(?:\s+([^>]+?))?\s*-->`)

// mdIgnoreFileObsidianRe matches `%% md:ignore-file [rule-a, rule-b] %%`.
var mdIgnoreFileObsidianRe = regexp.MustCompile(`%%\s*md:ignore-file(?:\s+([^%]+?))?\s*%%`)

// parseInlineSuppress scans body for suppression directives.
// Returns line-level suppressions and file-level rule ignores.
//
// Supported directives:
//   - md-disable-next-line <rules>: suppress next line (legacy)
//   - md:ignore [rules]: suppress next line (standalone) or same line (inline)
//   - md:ignore-file [rules]: suppress entire file
//
// When no rules are listed, all rules are suppressed (wildcard "*").
func parseInlineSuppress(body string, bodyOffset int) (map[int]map[string]bool, []string) {
	if body == "" {
		return nil, nil
	}

	out := make(map[int]map[string]bool)
	var fileIgnores []string

	for i, line := range strings.Split(body, "\n") {
		// Byte prefilter (gate ⊆ regex prerequisite): all 6 directive regexes
		// below anchor on either `<!--` (the three HTML forms) or `%%` (the three
		// Obsidian forms). Every match therefore requires a '<' or a '%' in the
		// line, so a line containing neither cannot match any of them — skipping
		// it is exact, not a heuristic. SIMD strings.IndexByte replaces 6 regex
		// scans on the overwhelming majority of prose lines. No state machine
		// precedes this loop, so skipping a line never desyncs later lines.
		if strings.IndexByte(line, '<') == -1 && strings.IndexByte(line, '%') == -1 {
			continue
		}

		// Legacy md-disable-next-line — always next-line.
		var legacyMatches [][]string
		legacyMatches = append(legacyMatches, inlineSuppressHTMLRe.FindAllStringSubmatch(line, -1)...)
		legacyMatches = append(legacyMatches, inlineSuppressObsidianRe.FindAllStringSubmatch(line, -1)...)
		for _, m := range legacyMatches {
			addRulesByCSV(out, bodyOffset+i+1, m[1])
		}

		// md:ignore-file — whole-file suppression.
		var fileMatches [][]string
		fileMatches = append(fileMatches, mdIgnoreFileHTMLRe.FindAllStringSubmatch(line, -1)...)
		fileMatches = append(fileMatches, mdIgnoreFileObsidianRe.FindAllStringSubmatch(line, -1)...)
		for _, m := range fileMatches {
			ruleStr := ""
			if len(m) > 1 {
				ruleStr = strings.TrimSpace(m[1])
			}
			if ruleStr == "" {
				fileIgnores = append(fileIgnores, "*")
			} else {
				for _, id := range strings.Split(ruleStr, ",") {
					id = strings.TrimSpace(id)
					if id != "" {
						fileIgnores = append(fileIgnores, id)
					}
				}
			}
		}

		// md:ignore — same-line or next-line depending on context.
		// Standalone directive (only directives on the line) → next-line.
		// Inline with other content → same-line.
		var ignoreMatches [][]string
		ignoreMatches = append(ignoreMatches, mdIgnoreHTMLRe.FindAllStringSubmatch(line, -1)...)
		ignoreMatches = append(ignoreMatches, mdIgnoreObsidianRe.FindAllStringSubmatch(line, -1)...)
		if len(ignoreMatches) > 0 {
			standalone := isMdIgnoreStandalone(line)
			targetLine := bodyOffset + i
			if standalone {
				targetLine = bodyOffset + i + 1
			}

			for _, m := range ignoreMatches {
				ruleStr := ""
				if len(m) > 1 {
					ruleStr = strings.TrimSpace(m[1])
				}
				if ruleStr == "" {
					addWildcard(out, targetLine)
				} else {
					addRulesByCSV(out, targetLine, ruleStr)
				}
			}
		}
	}

	if len(out) == 0 {
		out = nil
	}
	return out, fileIgnores
}

// isMdIgnoreStandalone reports whether a line contains only md:ignore
// directives and no other content. Standalone directives suppress the
// next line; inline directives suppress the current line.
func isMdIgnoreStandalone(line string) bool {
	cleaned := mdIgnoreHTMLRe.ReplaceAllString(line, "")
	cleaned = mdIgnoreObsidianRe.ReplaceAllString(cleaned, "")
	return strings.TrimSpace(cleaned) == ""
}

// addRulesByCSV parses a comma-separated rule ID list and adds each
// to the suppression set for the given line.
func addRulesByCSV(out map[int]map[string]bool, line int, csv string) {
	set := out[line]
	if set == nil {
		set = make(map[string]bool)
		out[line] = set
	}
	for _, id := range strings.Split(csv, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			set[id] = true
		}
	}
}

// addWildcard adds the wildcard sentinel "*" to suppress all rules on a line.
func addWildcard(out map[int]map[string]bool, line int) {
	set := out[line]
	if set == nil {
		set = make(map[string]bool)
		out[line] = set
	}
	set["*"] = true
}

// ScanOptions configures filesystem scanning behavior.
type ScanOptions struct {
	Skip        []string // directory skip patterns
	MaxFileSize int64    // max file size in bytes; 0 = no limit
}

// ScanWithOpts walks the filesystem and parses .md files into Documents.
// It accepts ScanOptions for skip patterns and max file size.
func ScanWithOpts(fsys fs.FS, opts ScanOptions) ([]*Document, error) {
	return scanImpl(fsys, opts.Skip, opts.MaxFileSize)
}

// Scan walks the filesystem and parses each .md file into a Document.
// Optional skip patterns cause directories to be skipped. An entry without
// a slash matches directories by name at any depth (e.g. "node_modules").
// An entry containing a slash is path-scoped: it matches the directory whose
// path relative to the scan root equals the entry with leading/trailing
// slashes trimmed (e.g. "/repos" skips only top-level repos/, not wiki/repos/).
func Scan(fsys fs.FS, skip ...string) ([]*Document, error) {
	return scanImpl(fsys, skip, 0)
}

// CollectPaths walks the filesystem collecting link-target paths without
// reading file content. This is much faster than a full scan when only
// the path list is needed (e.g. for __scanned_paths context).
//
// It collects .md documents and .base files (Obsidian Bases). Bases are
// first-class wikilink/embed targets — the wiki's designed ![[X.base#View]]
// catalog pattern depends on them resolving — but they are NOT markdown
// documents and are never parsed or checked (ScanFiles/scanImpl stay .md-only).
// Indexing them here is purely additive: it lets [[X.base]] resolve, and can
// never turn a currently-resolving link into a broken one.
func CollectPaths(fsys fs.FS, opts ScanOptions) ([]string, error) {
	skipNames := make(map[string]bool, len(opts.Skip))
	skipPaths := make(map[string]bool, len(opts.Skip))
	for _, s := range opts.Skip {
		if strings.Contains(s, "/") {
			skipPaths[strings.Trim(s, "/")] = true
		} else {
			skipNames[s] = true
		}
	}

	var paths []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && (skipNames[d.Name()] || skipPaths[path]) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".base") {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

// ScanFiles reads specific files by path and parses them into Documents.
// Non-.md paths and unreadable files are silently skipped.
func ScanFiles(fsys fs.FS, paths []string, maxFileSize int64) ([]*Document, error) {
	var docs []*Document
	for _, path := range paths {
		if !strings.HasSuffix(path, ".md") {
			continue
		}
		if maxFileSize > 0 {
			if info, err := fs.Stat(fsys, path); err == nil && info.Size() > maxFileSize {
				continue
			}
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			continue
		}

		doc := &Document{
			Path:        path,
			RawContent:  data,
			Frontmatter: make(map[string]any),
		}

		parsed, parseErr := frontmatter.ParseBytes(data)
		if parseErr != nil {
			docs = append(docs, doc)
			continue
		}
		if parsed != nil {
			doc.Frontmatter = parsed.Meta
			doc.Tags = parsed.Tags()
			doc.Body = parsed.Body
			doc.BodyOffset = parsed.BodyOffset
			doc.LintIgnore = parsed.StringListField("lint-ignore")
		} else {
			doc.Body = string(data)
			doc.BodyOffset = 1
		}
		lineSuppress, fileIgnores := parseInlineSuppress(doc.Body, doc.BodyOffset)
		doc.InlineSuppress = lineSuppress
		doc.LintIgnore = append(doc.LintIgnore, fileIgnores...)

		docs = append(docs, doc)
	}
	return docs, nil
}

func scanImpl(fsys fs.FS, skip []string, maxFileSize int64) ([]*Document, error) {
	skipNames := make(map[string]bool, len(skip))
	skipPaths := make(map[string]bool, len(skip))
	for _, s := range skip {
		if strings.Contains(s, "/") {
			skipPaths[strings.Trim(s, "/")] = true
		} else {
			skipNames[s] = true
		}
	}

	var docs []*Document

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && (skipNames[d.Name()] || skipPaths[path]) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		// One stat serves both the oversized-file guard and the cache stat
		// signature (size + mtime). On a stat error we neither skip nor record a
		// signature — a zero StatSig simply never engages the fast path.
		var sizeBytes, mtimeNs int64
		if info, statErr := d.Info(); statErr == nil {
			if maxFileSize > 0 && info.Size() > maxFileSize {
				return nil // skip oversized artifacts to avoid OOM
			}
			sizeBytes = info.Size()
			mtimeNs = info.ModTime().UnixNano()
		}

		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil // skip unreadable files
		}

		doc := &Document{
			Path:        path,
			RawContent:  data,
			Size:        sizeBytes,
			MtimeNs:     mtimeNs,
			Frontmatter: make(map[string]any),
		}

		parsed, err := frontmatter.ParseBytes(data)
		if err != nil {
			// Invalid frontmatter — still include doc with empty meta
			docs = append(docs, doc)
			return nil
		}
		if parsed != nil {
			doc.Frontmatter = parsed.Meta
			doc.Tags = parsed.Tags()
			doc.Body = parsed.Body
			doc.BodyOffset = parsed.BodyOffset
			doc.LintIgnore = parsed.StringListField("lint-ignore")
		} else {
			doc.Body = string(data)
			doc.BodyOffset = 1
		}
		lineSuppress, fileIgnores := parseInlineSuppress(doc.Body, doc.BodyOffset)
		doc.InlineSuppress = lineSuppress
		doc.LintIgnore = append(doc.LintIgnore, fileIgnores...)

		docs = append(docs, doc)
		return nil
	})

	return docs, err
}
