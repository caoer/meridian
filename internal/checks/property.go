package checks

import (
	"os/exec"
	"strings"
	"sync"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/property"
)

// gitRootOnce detects the git repository root once per process.
var (
	gitRootOnce  sync.Once
	gitRootValue string
)

// detectGitRoot finds the git repo root via git rev-parse.
// Cached after first call for the process lifetime.
func detectGitRoot() string {
	gitRootOnce.Do(func() {
		out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		if err == nil {
			gitRootValue = strings.TrimSpace(string(out))
		}
	})
	return gitRootValue
}

// resetGitRoot resets the cached git root (for testing only).
func resetGitRoot() {
	gitRootOnce = sync.Once{}
	gitRootValue = ""
}

// typeParsers is the registry of property type block parsers.
var typeParsers = map[string]property.TypeBlockParser{
	"wikilink": property.ParseWikilink,
	"tag":      property.ParseTag,
	"date":     property.ParseDate,
	"text":     property.ParseText,
}

// propertyCheck validates frontmatter properties using the property rule engine.
func propertyCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	cfg, err := property.Parse(params)
	if err != nil {
		return []engine.RawFinding{{
			Line: 0,
			TemplateData: map[string]string{
				"Message": "property config error: " + err.Error(),
				"Reason":  err.Error(),
			},
		}}
	}

	resolved, err := cfg.ResolveType(typeParsers)
	if err != nil {
		return []engine.RawFinding{{
			Line: 0,
			TemplateData: map[string]string{
				"Message": "type block error: " + err.Error(),
				"Reason":  err.Error(),
			},
		}}
	}

	scannedPaths, _ := params["__scanned_paths"].([]string)
	ctx := &property.EvalContext{
		ScannedPaths: scannedPaths,
		FilePath:     doc.Path,
		GitRoot:      detectGitRoot(),
	}

	var out []engine.RawFinding
	for _, key := range resolved.Keys {
		val, exists := doc.Frontmatter[key]

		if !exists && resolved.Required {
			out = append(out, engine.RawFinding{
				Line: 1,
				TemplateData: map[string]string{
					"Key":     key,
					"Reason":  "missing required field",
					"Message": key + ": missing required field",
				},
			})
			continue
		}
		if !exists {
			continue
		}

		if resolved.Type == nil {
			continue
		}

		out = append(out, evaluateValue(key, val, resolved.Type, ctx)...)
	}

	return out
}

// evaluateValue runs the type block against a frontmatter value, handling lists.
func evaluateValue(key string, val any, tb property.TypeBlock, ctx *property.EvalContext) []engine.RawFinding {
	var out []engine.RawFinding

	items, isList := val.([]any)
	if isList {
		for _, item := range items {
			out = append(out, convertFindings(key, tb.Evaluate(key, item, ctx))...)
		}
	} else {
		out = append(out, convertFindings(key, tb.Evaluate(key, val, ctx))...)
	}

	return out
}

// convertFindings maps property.Finding → engine.RawFinding, ensuring Message is populated.
func convertFindings(key string, findings []property.Finding) []engine.RawFinding {
	out := make([]engine.RawFinding, 0, len(findings))
	for _, f := range findings {
		td := f.TemplateData
		if td == nil {
			td = map[string]string{}
		}
		if td["Message"] == "" {
			td["Message"] = buildDefaultMessage(key, td)
		}
		out = append(out, engine.RawFinding{
			Line:         f.Line,
			TemplateData: td,
		})
	}
	return out
}

// buildDefaultMessage assembles a contextual default from TemplateData fields.
func buildDefaultMessage(key string, data map[string]string) string {
	if reason := data["Reason"]; reason != "" {
		target := data["Target"]
		if key != "" && target != "" {
			return key + ": " + target + " — " + reason
		}
		if key != "" {
			return key + ": " + reason
		}
		return reason
	}

	target := data["Target"]
	expected := data["Expected"]
	display := data["Display"]

	if target != "" && expected != "" {
		if display != "" {
			return key + ": [[" + target + "|" + display + "]] display should be " + expected
		}
		return key + ": [[" + target + "]] should have display " + expected
	}
	if target != "" {
		if value := data["Value"]; value != "" {
			return key + ": " + value + " target not found"
		}
		return key + ": " + target + " check failed"
	}

	if value := data["Value"]; value != "" {
		return key + ": invalid value " + value
	}

	return key + ": check failed"
}
