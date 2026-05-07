package fix

import (
	"io/fs"

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
	"field-exists": FieldExistsFix,
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

	// Track files we've already fixed (apply fixes cumulatively)
	fixedContent := make(map[string][]byte)

	for key, group := range grouped {
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
				continue
			}
			content = data
		}

		changed, newContent, actions, err := fixFn(content, rule.Params)
		if err != nil {
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

	// Write fixed files (unless dry-run)
	if !opts.DryRun {
		for path, content := range fixedContent {
			if err := fsys.WriteFile(path, content, 0644); err != nil {
				return report, err
			}
		}
	}

	return report, nil
}
