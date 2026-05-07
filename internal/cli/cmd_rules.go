package cli

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/rules"
)

// RuleSummary is the JSON representation of a rule in listings.
type RuleSummary struct {
	ID       string `json:"id"`
	Check    string `json:"check"`
	Severity string `json:"severity"`
	On       any    `json:"on"`
}

// RulesLsData is the data payload for rules ls.
type RulesLsData struct {
	Rules []RuleSummary `json:"rules"`
	Total int           `json:"total"`
}

// RulesLsHandler creates a handler for `md rules ls`.
func RulesLsHandler(loadedRules []rules.Rule) Handler {
	return func(req *Request) *Response {
		summaries := make([]RuleSummary, 0, len(loadedRules))
		for _, r := range loadedRules {
			summaries = append(summaries, RuleSummary{
				ID:       r.ID,
				Check:    r.Check,
				Severity: r.Severity.String(),
				On:       onFilterToAny(r.On),
			})
		}

		// Sort by ID for deterministic output
		sort.Slice(summaries, func(i, j int) bool {
			return summaries[i].ID < summaries[j].ID
		})

		return &Response{
			Version: ResponseVersion,
			Data: RulesLsData{
				Rules: summaries,
				Total: len(summaries),
			},
		}
	}
}

// onFilterToAny converts OnFilter to a JSON-friendly representation.
func onFilterToAny(f rules.OnFilter) any {
	var raw []string
	raw = append(raw, f.Paths...)
	for _, t := range f.Tags {
		raw = append(raw, "#"+t)
	}
	for _, e := range f.Excludes {
		if e.Path != "" {
			raw = append(raw, "!"+e.Path)
		}
		if e.Tag != "" {
			raw = append(raw, "!#"+e.Tag)
		}
	}
	if len(raw) == 1 {
		return raw[0]
	}
	return raw
}

// DebugData is the data payload for debug command.
type DebugData struct {
	Rule DebugRule `json:"rule"`
}

// DebugRule is the detailed rule info for debug.
type DebugRule struct {
	ID              string         `json:"id"`
	Check           string         `json:"check"`
	CheckRegistered bool           `json:"check_registered"`
	Severity        string         `json:"severity"`
	On              any            `json:"on"`
	Params          map[string]any `json:"params,omitempty"`
}

// DebugHandler creates a handler for `md debug`.
func DebugHandler(loadedRules []rules.Rule, registeredChecks map[string]bool) Handler {
	return func(req *Request) *Response {
		var params struct {
			Rule string `json:"rule"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return ErrorResponse(ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Rule == "" {
			return ErrorResponse(ErrInvalidParams, "debug requires 'rule' parameter")
		}

		// Find rule
		var found *rules.Rule
		for i := range loadedRules {
			if loadedRules[i].ID == params.Rule {
				found = &loadedRules[i]
				break
			}
		}
		if found == nil {
			return ErrorResponse(ErrInvalidParams, "unknown rule: "+params.Rule)
		}

		return &Response{
			Version: ResponseVersion,
			Data: DebugData{
				Rule: DebugRule{
					ID:              found.ID,
					Check:           found.Check,
					CheckRegistered: registeredChecks[found.Check],
					Severity:        found.Severity.String(),
					On:              onFilterToAny(found.On),
					Params:          found.Params,
				},
			},
		}
	}
}

// SearchResult is one item in help search results.
type SearchResult struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	Description string `json:"description"`
}

// HelpSearchData is the data payload for help search.
type HelpSearchData struct {
	Results []SearchResult `json:"results"`
}

// SearchRulesAndChecks searches rules and registered checks by query string.
// Called directly from main.go's searchFn closure.
func SearchRulesAndChecks(loadedRules []rules.Rule, registeredChecks map[string]bool, query string) HelpSearchData {
	var results []SearchResult

	for _, r := range loadedRules {
		if containsCI(r.ID, query) || containsCI(r.Check, query) || containsCI(r.Message, query) {
			results = append(results, SearchResult{
				Type:        "rule",
				ID:          r.ID,
				Description: r.Message,
			})
		}
	}

	for name := range registeredChecks {
		if containsCI(name, query) {
			dup := false
			for _, r := range results {
				if r.Type == "check" && r.ID == name {
					dup = true
					break
				}
			}
			if !dup {
				results = append(results, SearchResult{
					Type: "check",
					ID:   name,
				})
			}
		}
	}

	return HelpSearchData{Results: results}
}

// containsCI does case-insensitive substring check.
func containsCI(s, substr string) bool {
	return len(substr) > 0 && strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
