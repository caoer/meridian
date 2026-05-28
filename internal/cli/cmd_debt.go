package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// DebtSource is a scanned wiki document reduced to the fields the debt query
// needs. main.go builds these from engine documents (keeps cli free of an
// engine import).
type DebtSource struct {
	Path    string    // repo-relative path, e.g. wiki/sources/compound/foo.md
	Tags    []string  // frontmatter tags-list membership
	Created string    // frontmatter `created`, "" if absent
	ModTime time.Time // filesystem mtime, fallback sort key
}

// DebtEntry is one row of the incorporation-debt list. Mirrors DIGEST.md's
// columns: Source (path) · Where (folder) · Created (flagged date).
type DebtEntry struct {
	Source  string `json:"source"`
	Where   string `json:"where"`
	Created string `json:"created"`
}

// DebtData is the response payload for `md debt`.
type DebtData struct {
	Entries []DebtEntry `json:"entries"`
	Total   int         `json:"total"`
}

// NewDebtSource adapts a scanned document's fields into a DebtSource. Shared by
// the `md debt` handler and tests so both exercise the same created-extraction
// path (in particular, bare YAML dates that decode to time.Time).
func NewDebtSource(path string, tags []string, fm map[string]any, mod time.Time) DebtSource {
	return DebtSource{
		Path:    path,
		Tags:    tags,
		Created: createdString(fm["created"]),
		ModTime: mod,
	}
}

// createdString renders a frontmatter `created` value as a string. YAML decodes
// a bare ISO date (created: 2026-04-29) to time.Time and a quoted one to string.
func createdString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case time.Time:
		return t.Format("2006-01-02")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// FilterDebt selects sources under pathPrefix whose tags include tag, sorted
// newest-first by created (desc), falling back to ModTime (desc) when created
// values are equal or absent. This is the on-disk/agent-readable complement to
// DIGEST.md's Dataview query: `FROM "wiki/sources" AND #do/incorporate`.
func FilterDebt(sources []DebtSource, pathPrefix, tag string) DebtData {
	entries := make([]DebtEntry, 0)
	kept := make([]DebtSource, 0)

	for _, s := range sources {
		if !strings.HasPrefix(s.Path, pathPrefix) {
			continue
		}
		if !slices.Contains(s.Tags, tag) {
			continue
		}
		kept = append(kept, s)
	}

	sort.SliceStable(kept, func(i, j int) bool {
		a, b := kept[i], kept[j]
		if a.Created != b.Created {
			// ISO dates (YYYY-MM-DD) sort lexically; empty created sorts last.
			if a.Created == "" {
				return false
			}
			if b.Created == "" {
				return true
			}
			return a.Created > b.Created
		}
		if !a.ModTime.Equal(b.ModTime) {
			return a.ModTime.After(b.ModTime)
		}
		// Final deterministic tiebreak.
		return a.Path < b.Path
	})

	for _, s := range kept {
		entries = append(entries, DebtEntry{
			Source:  s.Path,
			Where:   filepath.Dir(s.Path),
			Created: s.Created,
		})
	}

	return DebtData{Entries: entries, Total: len(entries)}
}
