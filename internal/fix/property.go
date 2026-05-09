package fix

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/property"
)

// PropertyFix applies fixes for property rule findings.
// Fix 1: missing required field → add with default value.
// Fix 2: link_display mismatch → rewrite wikilink display text.
func PropertyFix(content []byte, params map[string]any) (bool, []byte, []string, error) {
	cfg, err := property.Parse(params)
	if err != nil {
		return false, content, nil, nil
	}

	changed := false
	result := content
	var actions []string

	// Fix 1: missing required fields
	if cfg.Required {
		c, r, a := fixMissingFields(result, cfg.Keys)
		if c {
			changed = true
			result = r
			actions = append(actions, a...)
		}
	}

	// Fix 2: link_display rewrite (only for wikilink type with link_display)
	if cfg.TypeName == "wikilink" {
		c, r, a := fixLinkDisplay(result, cfg)
		if c {
			changed = true
			result = r
			actions = append(actions, a...)
		}
	}

	return changed, result, actions, nil
}

// fixMissingFields adds missing required frontmatter fields with defaults.
func fixMissingFields(content []byte, keys []string) (bool, []byte, []string) {
	doc, err := frontmatter.ParseBytes(content)
	if err != nil {
		return false, content, nil
	}

	var missing []string
	if doc != nil {
		for _, k := range keys {
			if _, ok := doc.Meta[k]; !ok {
				missing = append(missing, k)
			}
		}
	} else {
		missing = keys
	}

	if len(missing) == 0 {
		return false, content, nil
	}

	var result string
	if doc != nil {
		result = insertFields(string(content), missing)
	} else {
		result = createFrontmatter(string(content), missing)
	}

	actions := make([]string, len(missing))
	for i, f := range missing {
		actions[i] = fmt.Sprintf("added property field '%s' with default value", f)
	}

	return true, []byte(result), actions
}

// fixLinkDisplay rewrites wikilink display text based on link_display templates.
func fixLinkDisplay(content []byte, cfg *property.Config) (bool, []byte, []string) {
	parsers := map[string]property.TypeBlockParser{
		"wikilink": property.ParseWikilink,
	}
	resolved, err := cfg.ResolveType(parsers)
	if err != nil || resolved.Type == nil {
		return false, content, nil
	}

	wb, ok := resolved.Type.(*property.WikilinkBlock)
	if !ok || wb.LinkDisplay == nil || len(wb.LinkDisplay.Templates) == 0 {
		return false, content, nil
	}

	doc, err := frontmatter.ParseBytes(content)
	if err != nil || doc == nil {
		return false, content, nil
	}

	changed := false
	result := string(content)
	var actions []string

	for _, key := range cfg.Keys {
		val, ok := doc.Meta[key]
		if !ok {
			continue
		}

		var texts []string
		switch v := val.(type) {
		case string:
			texts = []string{v}
		case []any:
			for _, item := range v {
				texts = append(texts, fmt.Sprintf("%v", item))
			}
		default:
			continue
		}

		for _, text := range texts {
			links := property.ParseWikilinks(text)
			for _, link := range links {
				expected := matchLinkDisplay(link, wb.LinkDisplay.Templates)
				if expected == "" || link.Display == expected {
					continue
				}

				var oldLink string
				if link.Display != "" {
					oldLink = "[[" + link.Target + "|" + link.Display + "]]"
				} else {
					oldLink = "[[" + link.Target + "]]"
				}
				newLink := "[[" + link.Target + "|" + expected + "]]"

				if strings.Contains(result, oldLink) {
					result = strings.Replace(result, oldLink, newLink, 1)
					changed = true
					actions = append(actions, fmt.Sprintf("rewrote %s -> %s", oldLink, newLink))
				}
			}
		}
	}

	if !changed {
		return false, content, nil
	}
	return true, []byte(result), actions
}

// matchLinkDisplay returns expected display text for a wikilink, or "" if no template matches.
func matchLinkDisplay(link property.Wikilink, templates []property.LinkDisplayTemplate) string {
	for _, tmpl := range templates {
		matched, _ := doublestar.Match(tmpl.When, link.Target)
		if !matched {
			continue
		}
		expected, err := renderLinkFormat(tmpl.Format, link.Target)
		if err != nil {
			continue
		}
		return expected
	}
	return ""
}

// renderLinkFormat delegates to property.ExecuteLinkTemplate.
func renderLinkFormat(format, target string) (string, error) {
	return property.ExecuteLinkTemplate(format, target)
}
