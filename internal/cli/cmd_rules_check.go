package cli

import (
	"encoding/json"

	"github.com/caoer/meridian/internal/conflict"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/rules"
)

// RulesCheckData is the data payload for `md rules check`.
type RulesCheckData struct {
	Conflicts              []RulesCheckConflict `json:"conflicts"`
	OverlapsWithoutConflict []RulesCheckOverlap `json:"overlaps_without_conflict"`
}

// RulesCheckConflict is one conflict in rules check output.
type RulesCheckConflict struct {
	RuleA    string `json:"rule_a"`
	RuleB    string `json:"rule_b"`
	Reason   string `json:"reason"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

// RulesCheckOverlap is one non-conflicting overlap in rules check output.
type RulesCheckOverlap struct {
	RuleA  string `json:"rule_a"`
	RuleB  string `json:"rule_b"`
	Reason string `json:"reason"`
}

// RulesCheckHandler creates a handler for `md rules check`.
// Detects overlapping on selectors and classifies conflicts.
func RulesCheckHandler(loadedRules []rules.Rule, profiles map[string]config.Profile) Handler {
	return func(req *Request) *Response {
		rr := loadedRules

		// Apply profile filter if specified.
		var params struct {
			Profile string `json:"profile"`
		}
		if req.Params != nil {
			json.Unmarshal(req.Params, &params)
		}
		if params.Profile != "" {
			if p, ok := profiles[params.Profile]; ok {
				rr = p.Resolve(rr)
			}
		}

		overlaps := conflict.DetectOverlaps(rr)
		conflicts := conflict.Classify(overlaps, rr)

		// Build conflict rule ID set for filtering overlaps.
		conflictPairs := make(map[[2]string]bool)
		var outConflicts []RulesCheckConflict
		for _, c := range conflicts {
			conflictPairs[[2]string{c.Overlap.RuleA, c.Overlap.RuleB}] = true
			outConflicts = append(outConflicts, RulesCheckConflict{
				RuleA:    c.Overlap.RuleA,
				RuleB:    c.Overlap.RuleB,
				Reason:   c.Overlap.Reason,
				Severity: c.Severity,
				Detail:   c.Detail,
			})
		}

		var outOverlaps []RulesCheckOverlap
		for _, o := range overlaps {
			if conflictPairs[[2]string{o.RuleA, o.RuleB}] {
				continue
			}
			outOverlaps = append(outOverlaps, RulesCheckOverlap{
				RuleA:  o.RuleA,
				RuleB:  o.RuleB,
				Reason: o.Reason,
			})
		}

		// Ensure non-nil slices for JSON output.
		if outConflicts == nil {
			outConflicts = []RulesCheckConflict{}
		}
		if outOverlaps == nil {
			outOverlaps = []RulesCheckOverlap{}
		}

		return &Response{
			Version: ResponseVersion,
			Data: RulesCheckData{
				Conflicts:              outConflicts,
				OverlapsWithoutConflict: outOverlaps,
			},
		}
	}
}
