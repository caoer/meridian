package fix

import (
	"io/fs"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
	"github.com/caoer/meridian/internal/vfs"
)

// FixFunc applies fixes for all findings of a check type on a file.
// content is the raw file bytes. params are the rule's check-specific params.
// Returns changed=true if any fixes applied, new content, action descriptions, and error.
type FixFunc func(content []byte, params map[string]any) (changed bool, newContent []byte, actions []string, err error)

// All is the registry of fix functions, keyed by check name.
var All = map[string]FixFunc{
	"field-exists":        FieldExistsFix,
	"property":            PropertyFix,
	"table-wikilink-pipe": TableWikilinkPipeFix,
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
func (f *Fixer) Fix(fsys vfs.WriteFS, ruleList []rules.Rule, opts Options) (*FixReport, error) {
	findings := f.engine.Run(fsys, ruleList)

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
			data, err := fs.ReadFile(fsys, key.path)
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

		changed, newContent, actions, err := fixFn(content, rule.Params)
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
		if !changed {
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
			if err := fsys.WriteFile(path, fixedContent[path], 0644); err != nil {
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
