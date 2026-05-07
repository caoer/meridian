package mv

import (
	"io/fs"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
)

// TagSuggestion recommends a tag change after a move.
type TagSuggestion struct {
	File   string `json:"file"`
	OldTag string `json:"old_tag"`
	NewTag string `json:"new_tag"`
}

// DetectTagMismatch checks if domain/ tags in frontmatter match the file's new path.
// Returns suggestions for mismatched tags.
func DetectTagMismatch(fsys fs.FS, paths []string) ([]TagSuggestion, error) {
	var suggestions []TagSuggestion

	for _, p := range paths {
		domain := domainFromPath(p)
		if domain == "" {
			continue // can't infer domain
		}

		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			continue
		}

		doc, err := frontmatter.ParseBytes(data)
		if err != nil || doc == nil {
			continue
		}

		tags := doc.Tags()
		for _, tag := range tags {
			if !strings.HasPrefix(tag, "domain/") {
				continue
			}
			tagDomain := strings.TrimPrefix(tag, "domain/")
			if tagDomain != domain {
				suggestions = append(suggestions, TagSuggestion{
					File:   p,
					OldTag: tag,
					NewTag: "domain/" + domain,
				})
			}
		}
	}

	return suggestions, nil
}

// domainFromPath extracts the domain from a wiki path.
// wiki/infra/page.md → "infra". Returns "" if not under wiki/ or at wiki root.
func domainFromPath(p string) string {
	if !strings.HasPrefix(p, "wiki/") {
		return ""
	}
	rest := strings.TrimPrefix(p, "wiki/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		return "" // file directly in wiki/, no subdomain
	}
	return parts[0]
}
