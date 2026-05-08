package engine

import (
	"bytes"
	"fmt"
	"io/fs"
	"sort"
	"text/template"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// RunCached is like Run but skips evaluation for files whose content+rules
// hash matches a cached entry. If store is nil, falls back to regular Run.
func (e *Engine) RunCached(fsys fs.FS, ruleList []rules.Rule, store *cache.Store) []types.Finding {
	if store == nil {
		return e.Run(fsys, ruleList)
	}

	e.warnings = nil

	docs, err := scan(fsys)
	if err != nil {
		e.warnings = append(e.warnings, types.Warning{
			Code:    "SCAN_ERROR",
			Message: fmt.Sprintf("scan error: %v", err),
		})
		return nil
	}

	// Pre-compute rule hashes for combined hash calculation.
	ruleHashes := make([]string, len(ruleList))
	for i, r := range ruleList {
		ruleHashes[i] = cache.RuleHash(r)
	}

	// Build scanned path list for checks that need resolution context.
	scannedPaths := make([]string, len(docs))
	for i, d := range docs {
		scannedPaths[i] = d.Path
	}

	// Pre-parse message templates for active rules.
	type activeRule struct {
		rule    rules.Rule
		checkFn CheckFunc
		tmpl    *template.Template
	}
	var active []activeRule
	for _, rule := range ruleList {
		if rule.Severity == rules.SeverityOff {
			continue
		}
		checkFn, ok := e.checks[rule.Check]
		if !ok {
			e.warnings = append(e.warnings, types.Warning{
				Code:    "CHECK_NOT_REGISTERED",
				Message: fmt.Sprintf("Rule '%s': check '%s' not registered, skipping", rule.ID, rule.Check),
			})
			continue
		}
		tmpl, err := template.New("").Parse(rule.Message)
		if err != nil {
			e.warnings = append(e.warnings, types.Warning{
				Code:    "TEMPLATE_ERROR",
				Message: fmt.Sprintf("Rule '%s': invalid template: %v", rule.ID, err),
			})
			continue
		}
		active = append(active, activeRule{rule: rule, checkFn: checkFn, tmpl: tmpl})
	}

	var findings []types.Finding

	for _, doc := range docs {
		// Compute combined hash for this file.
		raw, _ := fs.ReadFile(fsys, doc.Path)
		contentHash := cache.FileHash(raw)
		combined := cache.CombinedHash(contentHash, ruleHashes)

		// Cache check.
		if cached, ok := store.Get(doc.Path, combined); ok {
			findings = append(findings, cached...)
			continue
		}

		// Cache miss — evaluate all rules against this doc.
		var docFindings []types.Finding
		for _, ar := range active {
			if !Match(ar.rule.On, doc.Path, doc.Tags) {
				continue
			}
			if doc.IsIgnored(ar.rule.ID) {
				continue
			}

			effectiveParams := make(map[string]any, len(ar.rule.Params)+1)
			for k, v := range ar.rule.Params {
				effectiveParams[k] = v
			}
			effectiveParams["__scanned_paths"] = scannedPaths

			raws := ar.checkFn(doc, effectiveParams)
			for _, raw := range raws {
				var msgBuf bytes.Buffer
				if err := ar.tmpl.Execute(&msgBuf, raw.TemplateData); err != nil {
					msgBuf.Reset()
					msgBuf.WriteString(fmt.Sprintf("template error: %v", err))
				}
				docFindings = append(docFindings, types.Finding{
					RuleID:   ar.rule.ID,
					Severity: ar.rule.Severity.String(),
					FilePath: doc.Path,
					Line:     raw.Line,
					Column:   raw.Column,
					Message:  msgBuf.String(),
				})
			}
		}

		store.Put(doc.Path, combined, docFindings)
		findings = append(findings, docFindings...)
	}

	// Sort: file_path, rule_id, line.
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].FilePath != findings[j].FilePath {
			return findings[i].FilePath < findings[j].FilePath
		}
		if findings[i].RuleID != findings[j].RuleID {
			return findings[i].RuleID < findings[j].RuleID
		}
		return findings[i].Line < findings[j].Line
	})

	return findings
}
