package property

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

// WikilinkBlock implements TypeBlock for wikilink-typed properties.
type WikilinkBlock struct {
	Resolve     string             // file_exists | folder_exists | file_or_folder_exists | parent_exists
	Fresh       string             // "git" or "" (empty = no freshness check)
	LinkDisplay *LinkDisplayConfig // nil if no link_display
}

// LinkDisplayConfig holds template rules for expected wikilink display text.
type LinkDisplayConfig struct {
	Templates []LinkDisplayTemplate
}

// LinkDisplayTemplate maps a glob pattern to a format template.
type LinkDisplayTemplate struct {
	When   string // glob pattern against target path
	Format string // Go template for generating display text
}

// Wikilink is a parsed [[target|display]] pair.
type Wikilink struct {
	Target  string
	Display string
}

// validResolveModes enumerates accepted resolve predicates.
var validResolveModes = map[string]bool{
	"file_exists":           true,
	"folder_exists":         true,
	"file_or_folder_exists": true,
	"parent_exists":         true,
}

// wikilinkRe extracts [[target]] or [[target|display]] from text.
// Group 1 = target, Group 2 = display (optional).
var wikilinkRe = regexp.MustCompile(`\[\[([^\]\|]+)(?:\|([^\]]+))?\]\]`)

// ParseWikilinks extracts wikilink targets and display text from a string.
func ParseWikilinks(s string) []Wikilink {
	matches := wikilinkRe.FindAllStringSubmatch(s, -1)
	out := make([]Wikilink, 0, len(matches))
	for _, m := range matches {
		target := strings.TrimSpace(m[1])
		if target == "" {
			continue
		}
		wl := Wikilink{Target: target}
		if len(m) > 2 && m[2] != "" {
			wl.Display = strings.TrimSpace(m[2])
		}
		out = append(out, wl)
	}
	return out
}

// ParseWikilink converts raw YAML config into a WikilinkBlock (TypeBlockParser).
func ParseWikilink(raw any) (TypeBlock, error) {
	wb := &WikilinkBlock{}

	switch v := raw.(type) {
	case bool:
		if !v {
			return nil, fmt.Errorf("wikilink: false is not valid")
		}
		return wb, nil
	case map[string]any:
		if r, ok := v["resolve"]; ok {
			s, ok := r.(string)
			if !ok {
				return nil, fmt.Errorf("wikilink.resolve: expected string, got %T", r)
			}
			if !validResolveModes[s] {
				return nil, fmt.Errorf("wikilink.resolve: unknown mode %q", s)
			}
			wb.Resolve = s
		}
		if f, ok := v["fresh"]; ok {
			s, ok := f.(string)
			if !ok {
				return nil, fmt.Errorf("wikilink.fresh: expected string, got %T", f)
			}
			wb.Fresh = s
		}
		if ld, ok := v["link_display"]; ok {
			ldMap, ok := ld.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("wikilink.link_display: expected map, got %T", ld)
			}
			cfg, err := parseLinkDisplay(ldMap)
			if err != nil {
				return nil, err
			}
			wb.LinkDisplay = cfg
		}
		return wb, nil
	default:
		return nil, fmt.Errorf("wikilink: expected bool or map, got %T", raw)
	}
}

func parseLinkDisplay(m map[string]any) (*LinkDisplayConfig, error) {
	raw, ok := m["template"]
	if !ok {
		return &LinkDisplayConfig{}, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("link_display.template: expected list, got %T", raw)
	}
	cfg := &LinkDisplayConfig{}
	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("link_display.template[%d]: expected map, got %T", i, item)
		}
		when, _ := entry["when"].(string)
		format, _ := entry["format"].(string)
		cfg.Templates = append(cfg.Templates, LinkDisplayTemplate{When: when, Format: format})
	}
	return cfg, nil
}

// BuildResolveIndex creates a case-insensitive lookup from scanned paths.
// Keys are lowercased stems (file_exists) or directory components (folder/parent).
func BuildResolveIndex(paths []string, mode string) map[string]bool {
	idx := make(map[string]bool, len(paths))
	switch mode {
	case "file_exists":
		for _, p := range paths {
			if filepath.IsAbs(p) {
				continue
			}
			stem := strings.TrimSuffix(filepath.Base(p), ".md")
			idx[strings.ToLower(stem)] = true
			rel := strings.TrimSuffix(p, ".md")
			idx[strings.ToLower(rel)] = true
		}
	case "folder_exists":
		for _, p := range paths {
			if filepath.IsAbs(p) {
				continue
			}
			dir := filepath.Dir(p)
			for dir != "." && dir != "" {
				idx[strings.ToLower(dir)] = true
				idx[strings.ToLower(filepath.Base(dir))] = true
				dir = filepath.Dir(dir)
			}
		}
	case "parent_exists":
		for _, p := range paths {
			if filepath.IsAbs(p) {
				continue
			}
			dir := filepath.Dir(p)
			if dir != "." && dir != "" {
				idx[strings.ToLower(dir)] = true
				idx[strings.ToLower(filepath.Base(dir))] = true
			}
		}
	case "file_or_folder_exists":
		for _, p := range paths {
			if filepath.IsAbs(p) {
				continue
			}
			stem := strings.TrimSuffix(filepath.Base(p), ".md")
			idx[strings.ToLower(stem)] = true
			rel := strings.TrimSuffix(p, ".md")
			idx[strings.ToLower(rel)] = true
			dir := filepath.Dir(p)
			for dir != "." && dir != "" {
				idx[strings.ToLower(dir)] = true
				idx[strings.ToLower(filepath.Base(dir))] = true
				dir = filepath.Dir(dir)
			}
		}
	}
	return idx
}

// Evaluate checks wikilinks in value against configured predicates.
func (wb *WikilinkBlock) Evaluate(key string, value any, ctx *EvalContext) []Finding {
	if value == nil {
		return nil
	}

	// Extract text strings from value.
	var texts []string
	switch v := value.(type) {
	case string:
		texts = []string{v}
	case []any:
		for _, item := range v {
			texts = append(texts, fmt.Sprintf("%v", item))
		}
	default:
		texts = []string{fmt.Sprintf("%v", value)}
	}

	// Parse all wikilinks across texts. If no [[...]] syntax found,
	// treat the bare text as a wikilink target (matches Obsidian behavior
	// where frontmatter values like "secrets" resolve to wiki pages).
	var links []Wikilink
	for _, t := range texts {
		parsed := ParseWikilinks(t)
		if len(parsed) > 0 {
			links = append(links, parsed...)
		} else {
			trimmed := strings.TrimSpace(t)
			if trimmed != "" {
				links = append(links, Wikilink{Target: trimmed})
			}
		}
	}
	if len(links) == 0 {
		return nil
	}

	// Pre-build resolve index once (if needed).
	var resolveIdx map[string]bool
	if wb.Resolve != "" {
		resolveIdx = BuildResolveIndex(ctx.ScannedPaths, wb.Resolve)
	}

	var findings []Finding
	for _, link := range links {
		if wb.Resolve != "" {
			if !resolveCheck(link.Target, resolveIdx, wb.Resolve) {
				findings = append(findings, Finding{
					Line: 1,
					TemplateData: map[string]string{
						"Key":    key,
						"Value":  formatWikilink(link),
						"Target": link.Target,
						"Reason": wb.Resolve + ": target not found",
					},
				})
			}
		}

		if wb.Fresh == "git" {
			findings = append(findings, wb.checkFresh(key, link, ctx)...)
		}

		if wb.LinkDisplay != nil {
			findings = append(findings, wb.checkLinkDisplay(key, link)...)
		}
	}

	return findings
}

// resolveCheck looks up a wikilink target in the resolve index.
func resolveCheck(target string, idx map[string]bool, mode string) bool {
	switch mode {
	case "parent_exists":
		parent := filepath.Dir(target)
		if parent == "." || parent == "" {
			return true // root-level target — no parent to check
		}
		return idx[strings.ToLower(parent)]
	default:
		return idx[strings.ToLower(target)]
	}
}

func (wb *WikilinkBlock) checkFresh(key string, link Wikilink, ctx *EvalContext) []Finding {
	if ctx.GitRoot == "" {
		return nil // gracefully skip when no git root
	}

	targetPath := resolveTargetPath(link.Target, ctx.ScannedPaths)
	if targetPath == "" {
		return nil // can't check freshness if target doesn't resolve
	}

	sourceTime := ctx.gitTime(ctx.FilePath)
	targetTime := ctx.gitTime(targetPath)

	if sourceTime == 0 || targetTime == 0 {
		return nil // git timestamp unavailable
	}

	if targetTime > sourceTime {
		return []Finding{{
			Line: 1,
			TemplateData: map[string]string{
				"Key":    key,
				"Target": link.Target,
				"Value":  formatWikilink(link),
			},
		}}
	}
	return nil
}

// resolveTargetPath finds actual file path for a wikilink target via stem match.
func resolveTargetPath(target string, paths []string) string {
	lower := strings.ToLower(target)
	for _, p := range paths {
		stem := strings.TrimSuffix(filepath.Base(p), ".md")
		if strings.ToLower(stem) == lower {
			return p
		}
	}
	return ""
}

// gitTime returns a cached git timestamp for path, calling git only on cache miss.
func (ctx *EvalContext) gitTime(path string) int64 {
	if ctx.GitTimeCache != nil {
		if t, ok := ctx.GitTimeCache[path]; ok {
			return t
		}
	}
	t := gitLastCommitTime(ctx.GitRoot, path)
	if ctx.GitTimeCache == nil {
		ctx.GitTimeCache = make(map[string]int64)
	}
	ctx.GitTimeCache[path] = t
	return t
}

// gitLastCommitTime returns unix timestamp of last commit touching filePath.
func gitLastCommitTime(gitRoot, filePath string) int64 {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", gitRoot, "log", "-1", "--format=%ct", "--", filePath)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0
	}
	ts, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return ts
}

func (wb *WikilinkBlock) checkLinkDisplay(key string, link Wikilink) []Finding {
	if len(wb.LinkDisplay.Templates) == 0 {
		return nil
	}

	for _, tmpl := range wb.LinkDisplay.Templates {
		matched, _ := doublestar.Match(tmpl.When, link.Target)
		if !matched {
			continue
		}

		expected, err := ExecuteLinkTemplate(tmpl.Format, link.Target)
		if err != nil {
			return []Finding{{
				Line: 1,
				TemplateData: map[string]string{
					"Key":    key,
					"Target": link.Target,
					"Error":  err.Error(),
				},
			}}
		}

		if link.Display != expected {
			return []Finding{{
				Line: 1,
				TemplateData: map[string]string{
					"Key":      key,
					"Target":   link.Target,
					"Display":  link.Display,
					"Expected": expected,
				},
			}}
		}
		return nil // matched and correct
	}

	return nil // no template matched — no change required
}

// LinkDisplayFuncMap returns template functions available in link_display format strings.
// Pipe-friendly: prefix/suffix as first arg, piped value as second.
func LinkDisplayFuncMap() template.FuncMap {
	return template.FuncMap{
		"trimPrefix": func(prefix, s string) string { return strings.TrimPrefix(s, prefix) },
		"trimSuffix": func(suffix, s string) string { return strings.TrimSuffix(s, suffix) },
		"base":       filepath.Base,
		"dir":        filepath.Dir,
	}
}

// ExecuteLinkTemplate runs a link_display format template against a target path.
func ExecuteLinkTemplate(format, target string) (string, error) {
	base := LinkDisplayFuncMap()
	funcs := make(template.FuncMap, len(base)+1)
	for k, v := range base {
		funcs[k] = v
	}
	funcs["target"] = func() string { return target }

	tmpl, err := template.New("").Funcs(funcs).Parse(format)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	data := map[string]string{"Target": target}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func formatWikilink(wl Wikilink) string {
	if wl.Display != "" {
		return "[[" + wl.Target + "|" + wl.Display + "]]"
	}
	return "[[" + wl.Target + "]]"
}
