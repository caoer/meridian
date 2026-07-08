package fix

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
	"github.com/caoer/meridian/internal/vfs"
)

// ErrDryRunWrite is returned if a write is attempted during a dry run.
// This should never happen in correct code — it exists as a safety net.
var ErrDryRunWrite = errors.New("write attempted during dry-run")

// ScannedPathsKey is the parameter key injected into fix params, matching the
// engine check path convention. Exported so downstream fixers can reference it
// without magic strings.
const ScannedPathsKey = "__scanned_paths"

// FixFunc applies fixes for all findings of a check type on a file.
// content is the raw file bytes. params are the rule's check-specific params.
// Returns changed=true if any fixes applied, new content, action descriptions, and error.
type FixFunc func(content []byte, params map[string]any) (changed bool, newContent []byte, actions []string, err error)

// All is the registry of fix functions, keyed by check name.
var All = map[string]FixFunc{
	"field-exists":          FieldExistsFix,
	"property":              PropertyFix,
	"table-wikilink-pipe":   TableWikilinkPipeFix,
	"wikilink-canonicalize": WikilinkCanonicalizeFix,
}

// ParamSpec declares accepted param names and types for a fixer.
// Used for strict validation — unknown keys and type mismatches are hard errors.
type ParamSpec struct {
	// Accepted maps param name → expected Go type string (e.g. "[]string", "map[string]any").
	// Empty string means any type is accepted.
	Accepted map[string]string
}

// ParamSpecs is the registry of accepted params per check name.
// Checks not listed here skip param validation (backward compatible).
var ParamSpecs = map[string]ParamSpec{
	"wikilink-canonicalize": {
		Accepted: map[string]string{
			"roots":          "",
			"skip-prefixes":  "",
			"resolved_links": "map[string]any",
			"new_files":      "",
		},
	},
	"field-exists": {
		Accepted: map[string]string{
			"frontmatter": "",
		},
	},
	"broken-wikilink": {
		Accepted: map[string]string{
			"roots":         "",
			"scope":         "",
			"skip-prefixes": "",
		},
	},
	"ambiguous-wikilink": {
		Accepted: map[string]string{
			"roots":         "",
			"skip-prefixes": "",
		},
	},
	"table-wikilink-pipe": {
		Accepted: map[string]string{},
	},
	"link-resolve": {
		Accepted: map[string]string{
			"frontmatter":    "",
			"roots":          "",
			"resolved_index": "",
		},
	},
	"property": {
		Accepted: map[string]string{
			"wikilink": "",
			"tag":      "",
			"text":     "",
			"date":     "",
		},
	},
	"tier-downgrade": {
		Accepted: map[string]string{
			"foreign-roots": "",
		},
	},
	"tag-format": {
		Accepted: map[string]string{
			"prefixes": "",
		},
	},
	"pattern": {
		Accepted: map[string]string{
			"target": "",
			"match":  "",
		},
	},
	"effect-pin-resolves":        {Accepted: map[string]string{"absent-repo": ""}},
	"effect-pin-on-origin":       {Accepted: map[string]string{"absent-repo": ""}},
	"effect-checksum-reproduces": {Accepted: map[string]string{"absent-repo": ""}},
	"effect-pin-stale":           {Accepted: map[string]string{"absent-repo": ""}},
	"effect-unpinned":            {Accepted: map[string]string{"absent-repo": ""}},
	"heading-structure": {
		Accepted: map[string]string{
			"allow_multiple_h1": "",
		},
	},
	"backticked-wikilink": {
		Accepted: map[string]string{},
	},
	"partition-date-consistency": {
		Accepted: map[string]string{
			"date_field": "",
		},
	},
	"partition-rollup": {
		Accepted: map[string]string{
			"date_field":      "",
			"daily_max_days":  "",
			"weekly_max_days": "",
			"reference_time":  "",
		},
	},
}

// validateParams checks that all non-injected params in a rule are declared
// in the ParamSpec for that check. Returns an error naming the unknown key
// and suggesting the nearest valid alternative.
func validateParams(checkName string, params map[string]any) error {
	spec, ok := ParamSpecs[checkName]
	if !ok {
		return nil // no spec registered — skip validation
	}

	for key, val := range params {
		// Skip engine-injected params (__ prefix).
		if len(key) >= 2 && key[:2] == "__" {
			continue
		}

		expectedType, accepted := spec.Accepted[key]
		if !accepted {
			suggestion := nearestKey(key, spec.Accepted)
			if suggestion != "" {
				return fmt.Errorf("unknown param %q for check %q (did you mean %q?)", key, checkName, suggestion)
			}
			return fmt.Errorf("unknown param %q for check %q", key, checkName)
		}

		// Type validation for params with declared types.
		if expectedType != "" && val != nil {
			if err := checkParamType(key, val, expectedType); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkParamType validates that val matches the expected type string.
func checkParamType(key string, val any, expected string) error {
	actual := fmt.Sprintf("%T", val)
	switch expected {
	case "map[string]any":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("param %q: expected %s, got %s", key, expected, actual)
		}
	case "[]string":
		switch val.(type) {
		case []string, []any:
			// both acceptable (YAML produces []any)
		default:
			return fmt.Errorf("param %q: expected %s, got %s", key, expected, actual)
		}
	}
	return nil
}

// nearestKey finds the most similar key in accepted using simple edit distance.
func nearestKey(input string, accepted map[string]string) string {
	best := ""
	bestDist := len(input) + 1
	for key := range accepted {
		d := editDist(input, key)
		if d < bestDist && d <= 3 { // threshold: max 3 edits
			bestDist = d
			best = key
		}
	}
	return best
}

func editDist(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr := make([]int, lb+1)
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = curr
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// FixResult describes one fix applied.
type FixResult struct {
	FilePath string `json:"file_path"`
	RuleID   string `json:"rule_id"`
	Action   string `json:"action"`
}

// SkipResult describes an unfixable finding.
type SkipResult struct {
	FilePath string `json:"file_path"`
	RuleID   string `json:"rule_id"`
	Reason   string `json:"reason"`
}

// FixReport is the output of a fix run.
type FixReport struct {
	Fixed          []FixResult  `json:"fixed"`
	Unfixable      []SkipResult `json:"unfixable"`
	FixedCount     int          `json:"fixed_count"`
	UnfixableCount int          `json:"unfixable_count"`
}

// Options controls fix behavior.
type Options struct {
	DryRun bool
	Scope  string
	// Files is an explicit file universe (vault index). When non-nil, these
	// paths are injected as __scanned_paths into fix params — mirroring how
	// the engine check path injects the vault index at evalDoc time. If nil,
	// the Fixer scans the filesystem to derive the list automatically.
	Files []string
}

// Fixer runs checks and applies registered fixes.
type Fixer struct {
	engine   *engine.Engine
	registry map[string]FixFunc
}

// New creates a Fixer.
func New(eng *engine.Engine, registry map[string]FixFunc) *Fixer {
	return &Fixer{engine: eng, registry: registry}
}

// Fix runs checks, applies fixes for fixable findings, writes results.
// When opts.DryRun is true, the filesystem is wrapped in a read-only guard
// that makes writes physically impossible — belt-and-suspenders defense.
func (f *Fixer) Fix(fsys vfs.WriteFS, ruleList []rules.Rule, opts Options) (*FixReport, error) {
	// Belt-and-suspenders: when DryRun is true, wrap the FS so WriteFile
	// is physically impossible, not just gated by an if-check.
	writeFS := fsys
	if opts.DryRun {
		writeFS = &readOnlyFS{inner: fsys}
	}

	// Early param validation: reject unknown keys and type mismatches
	// BEFORE running checks — a bad param can cause the check to silently
	// produce no findings, hiding the error.
	for _, rule := range ruleList {
		if _, hasSpec := ParamSpecs[rule.Check]; hasSpec {
			if err := validateParams(rule.Check, rule.Params); err != nil {
				return &FixReport{
					Unfixable: []SkipResult{{
						FilePath: "(all)",
						RuleID:   rule.ID,
						Reason:   "param error: " + err.Error(),
					}},
					UnfixableCount: 1,
				}, fmt.Errorf("param validation failed for rule %q: %w", rule.ID, err)
			}
		}
	}

	findings := f.engine.Run(writeFS, ruleList)

	// Resolve scanned paths (vault index) for fixers that need cross-file context.
	// Mirrors engine/cached.go:153 injection of __scanned_paths.
	scannedPaths := opts.Files
	if scannedPaths == nil {
		docs, err := engine.Scan(writeFS)
		if err == nil {
			scannedPaths = make([]string, len(docs))
			for i, d := range docs {
				scannedPaths[i] = d.Path
			}
		} else {
			// Graceful degradation: fixers that don't need the index still work.
			scannedPaths = []string{}
		}
	}

	// Pre-filter findings by scope so only scoped files get modified.
	if opts.Scope != "" {
		dirPrefix := opts.Scope
		if !strings.HasSuffix(dirPrefix, "/") {
			dirPrefix += "/"
		}
		var scoped []types.Finding
		for _, finding := range findings {
			if finding.FilePath == opts.Scope ||
				finding.FilePath == opts.Scope+".md" ||
				strings.HasPrefix(finding.FilePath, dirPrefix) {
				scoped = append(scoped, finding)
			}
		}
		findings = scoped
	}

	if len(findings) == 0 {
		return &FixReport{}, nil
	}

	// Build rule lookup: ruleID → rule
	ruleMap := make(map[string]rules.Rule, len(ruleList))
	for _, r := range ruleList {
		ruleMap[r.ID] = r
	}

	// Group findings by (filePath, ruleID)
	type fileRuleKey struct {
		path   string
		ruleID string
	}
	grouped := make(map[fileRuleKey][]types.Finding)
	for _, finding := range findings {
		key := fileRuleKey{path: finding.FilePath, ruleID: finding.RuleID}
		grouped[key] = append(grouped[key], finding)
	}

	report := &FixReport{}

	// Fix #4: Deterministic iteration order — sort keys.
	keys := make([]fileRuleKey, 0, len(grouped))
	for k := range grouped {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].path != keys[j].path {
			return keys[i].path < keys[j].path
		}
		return keys[i].ruleID < keys[j].ruleID
	})

	// Track files we've already fixed (apply fixes cumulatively)
	fixedContent := make(map[string][]byte)

	for _, key := range keys {
		group := grouped[key]
		rule, ok := ruleMap[key.ruleID]
		if !ok {
			continue
		}

		fixFn, fixable := f.registry[rule.Check]
		if !fixable {
			for range group {
				report.Unfixable = append(report.Unfixable, SkipResult{
					FilePath: key.path,
					RuleID:   key.ruleID,
					Reason:   "no auto-fix available",
				})
				report.UnfixableCount++
			}
			continue
		}

		// Read file content (use previously fixed content if available)
		content, ok := fixedContent[key.path]
		if !ok {
			data, err := fs.ReadFile(writeFS, key.path)
			if err != nil {
				// Fix #5: Report read errors instead of silently dropping.
				report.Unfixable = append(report.Unfixable, SkipResult{
					FilePath: key.path,
					RuleID:   key.ruleID,
					Reason:   "read error: " + err.Error(),
				})
				report.UnfixableCount++
				continue
			}
			content = data
		}

		// Build effective params with __scanned_paths injected — mirrors
		// engine/cached.go evalDoc injection so fixers have vault context.
		effectiveParams := make(map[string]any, len(rule.Params)+1)
		for k, v := range rule.Params {
			effectiveParams[k] = v
		}
		effectiveParams["__scanned_paths"] = scannedPaths
		effectiveParams["__file_path"] = key.path

		// Strict param validation: reject unknown keys and type mismatches.
		if err := validateParams(rule.Check, effectiveParams); err != nil {
			report.Unfixable = append(report.Unfixable, SkipResult{
				FilePath: key.path,
				RuleID:   key.ruleID,
				Reason:   "param error: " + err.Error(),
			})
			report.UnfixableCount++
			continue
		}

		changed, newContent, actions, err := fixFn(content, effectiveParams)
		if err != nil {
			// Fix #5: Report fix errors instead of silently dropping.
			report.Unfixable = append(report.Unfixable, SkipResult{
				FilePath: key.path,
				RuleID:   key.ruleID,
				Reason:   "fix error: " + err.Error(),
			})
			report.UnfixableCount++
			continue
		}
		// P1-1: Process actions even when content is unchanged.
		// Unchanged actions (e.g. AMBIGUOUS reports) go to Unfixable;
		// changed actions (actual rewrites) go to Fixed.
		if !changed {
			for _, action := range actions {
				report.Unfixable = append(report.Unfixable, SkipResult{
					FilePath: key.path,
					RuleID:   key.ruleID,
					Reason:   action,
				})
				report.UnfixableCount++
			}
			continue
		}

		fixedContent[key.path] = newContent

		for _, action := range actions {
			report.Fixed = append(report.Fixed, FixResult{
				FilePath: key.path,
				RuleID:   key.ruleID,
				Action:   action,
			})
			report.FixedCount++
		}
	}

	// Fix #6: Write in deterministic order, track failures instead of returning early.
	if !opts.DryRun {
		writePaths := make([]string, 0, len(fixedContent))
		for p := range fixedContent {
			writePaths = append(writePaths, p)
		}
		sort.Strings(writePaths)

		for _, path := range writePaths {
			if err := writeFS.WriteFile(path, fixedContent[path], 0644); err != nil {
				// Move this file's fixes from Fixed to Unfixable.
				var kept []FixResult
				for _, f := range report.Fixed {
					if f.FilePath == path {
						report.Unfixable = append(report.Unfixable, SkipResult{
							FilePath: f.FilePath,
							RuleID:   f.RuleID,
							Reason:   "write error: " + err.Error(),
						})
						report.UnfixableCount++
					} else {
						kept = append(kept, f)
					}
				}
				report.Fixed = kept
				report.FixedCount = len(kept)
			}
		}
	}

	return report, nil
}

// readOnlyFS wraps a WriteFS and rejects all mutations.
// Used as a safety net during dry-run to make writes physically impossible.
type readOnlyFS struct {
	inner vfs.WriteFS
}

func (r *readOnlyFS) Open(name string) (fs.File, error)           { return r.inner.Open(name) }
func (r *readOnlyFS) WriteFile(string, []byte, fs.FileMode) error { return ErrDryRunWrite }
func (r *readOnlyFS) MkdirAll(string, fs.FileMode) error          { return ErrDryRunWrite }
func (r *readOnlyFS) Remove(string) error                         { return ErrDryRunWrite }
func (r *readOnlyFS) Rename(string, string) error                 { return ErrDryRunWrite }
func (r *readOnlyFS) ReadDir(name string) ([]fs.DirEntry, error)  { return fs.ReadDir(r.inner, name) }
func (r *readOnlyFS) Stat(name string) (fs.FileInfo, error) {
	return fs.Stat(r.inner, name)
}

var _ vfs.WriteFS = (*readOnlyFS)(nil) // compile-time interface check

// ReadDirFile exposes ReadDir for engine.Scan which needs fs.ReadDirFS.
func (r *readOnlyFS) ReadDirFile(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(r.inner, name)
}
