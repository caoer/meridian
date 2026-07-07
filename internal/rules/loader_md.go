package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/run"
)

// MDRuleKey is the frontmatter key on a literate rule page addressing the
// rule's yaml fence: `md-rule: "[[page#^rule]]"` — the same frontmatter-key →
// ^id-block convention as md-run's `md-<name>` task keys. The wikilink target
// must be the page itself (or empty, `[[#^rule]]`): a rule definition never
// lives in another file.
const MDRuleKey = "md-rule"

// loadMarkdownFile loads a literate rule page. The extracted yaml fence goes
// through the exact same pipeline as a .yaml rule file (complexity guard,
// raw-map decode, check/property routing); the rule ID is the filename stem.
//
// A page without an md-rule key is a doc page, not a rule — skipped
// (ok=false) with a warning, so a typo'd key cannot silently disable
// enforcement while a pack README stays legal.
func loadMarkdownFile(path string) (Rule, bool, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Rule{}, false, nil, err
	}

	doc, err := frontmatter.ParseBytes(data)
	if err != nil {
		return Rule{}, false, nil, fmt.Errorf("frontmatter: %w", err)
	}
	if doc == nil || !doc.HasField(MDRuleKey) {
		warn := fmt.Sprintf("%s: no %q frontmatter key — treated as doc page, no rule loaded",
			filepath.Base(path), MDRuleKey)
		return Rule{}, false, []string{warn}, nil
	}

	refStr, ok := doc.Meta[MDRuleKey].(string)
	if !ok {
		return Rule{}, false, nil, fmt.Errorf("%s must be a string wikilink (\"[[page#^id]]\"), got %T",
			MDRuleKey, doc.Meta[MDRuleKey])
	}
	link, err := run.ParseWikilink(refStr)
	if err != nil {
		return Rule{}, false, nil, fmt.Errorf("%s: %w", MDRuleKey, err)
	}
	if link.BlockID == "" {
		return Rule{}, false, nil, fmt.Errorf("%s must address a block: \"[[page#^id]]\", got %q", MDRuleKey, refStr)
	}
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if target := strings.TrimSuffix(filepath.Base(link.Target), ".md"); link.Target != "" && !strings.EqualFold(target, stem) {
		return Rule{}, false, nil, fmt.Errorf("%s target %q is not this page (%s) — rule definitions live in their own page",
			MDRuleKey, link.Target, stem)
	}

	block, err := run.FindBlock(string(data), link.BlockID)
	if err != nil {
		return Rule{}, false, nil, fmt.Errorf("%s: %w", MDRuleKey, err)
	}
	if !block.Fence || (block.Lang != "yaml" && block.Lang != "yml") {
		lang := block.Lang
		if !block.Fence {
			lang = "not a fence"
		}
		return Rule{}, false, nil, fmt.Errorf("block ^%s must be a yaml fence, got %s", link.BlockID, lang)
	}

	r, warns, err := loadYAMLBytes([]byte(block.Code), path)
	if err != nil {
		return Rule{}, false, nil, err
	}
	return r, true, warns, nil
}
