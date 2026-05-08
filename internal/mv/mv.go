// Package mv implements wiki file/folder move with cross-wiki link update,
// post-move re-lint, and tag mismatch detection.
package mv

import (
	"io/fs"
	"strings"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
	"github.com/caoer/meridian/internal/vfs"
)

// MoveResult holds the outcome of a move operation.
type MoveResult struct {
	Source         string          `json:"source"`
	Destination    string          `json:"destination"`
	FilesCount     int             `json:"files_count"`
	LinkUpdates    []LinkUpdate    `json:"link_updates,omitempty"`
	RelintFindings []types.Finding `json:"relint_findings,omitempty"`
	TagSuggestions []TagSuggestion `json:"tag_suggestions,omitempty"`
	Warnings       []string        `json:"warnings,omitempty"`
}

// Move orchestrates a complete file move with link update, re-lint, and tag check.
// If dryRun is true, computes the result without writing anything.
func Move(fsys vfs.WriteFS, source, dest string, eng *engine.Engine, ruleList []rules.Rule, dryRun bool) (*MoveResult, error) {
	result := &MoveResult{
		Source:      source,
		Destination: dest,
	}

	if dryRun {
		return dryRunMove(fsys, source, dest, eng, ruleList, result)
	}

	// Step 1: Move files
	movedFiles, err := MoveFiles(fsys, source, dest)
	if err != nil {
		return nil, err
	}
	result.FilesCount = len(movedFiles)

	// Step 2: Build stem map and update links
	stemMap := buildStemMap(source, dest, movedFiles)
	updates, err := UpdateLinks(fsys, stemMap)
	if err != nil {
		result.Warnings = append(result.Warnings, "link update error: "+err.Error())
	} else {
		result.LinkUpdates = updates
	}

	// Step 3: Re-lint moved files at new locations
	if eng != nil && len(ruleList) > 0 {
		result.RelintFindings = Relint(fsys, eng, ruleList, movedFiles)
	}

	// Step 4: Tag mismatch detection
	suggestions, err := DetectTagMismatch(fsys, movedFiles)
	if err != nil {
		result.Warnings = append(result.Warnings, "tag check error: "+err.Error())
	} else {
		result.TagSuggestions = suggestions
	}

	return result, nil
}

// dryRunMove computes what would happen without writing.
func dryRunMove(fsys vfs.WriteFS, source, dest string, eng *engine.Engine, ruleList []rules.Rule, result *MoveResult) (*MoveResult, error) {
	// Check source exists
	srcInfo, err := fs.Stat(fsys, source)
	if err != nil {
		return nil, err
	}

	// Check dest doesn't exist
	if _, err := fs.Stat(fsys, dest); err == nil {
		return nil, &fs.PathError{Op: "move", Path: dest, Err: fs.ErrExist}
	}

	// Enumerate files that would move
	var srcFiles []string
	if srcInfo.IsDir() {
		fs.WalkDir(fsys, source, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				srcFiles = append(srcFiles, p)
			}
			return nil
		})
	} else {
		srcFiles = []string{source}
	}

	// Compute dest paths
	destFiles := make([]string, len(srcFiles))
	for i, f := range srcFiles {
		if srcInfo.IsDir() {
			rel := strings.TrimPrefix(f, source+"/")
			destFiles[i] = dest + "/" + rel
		} else {
			destFiles[i] = dest
		}
	}
	result.FilesCount = len(srcFiles)

	// Compute stem map
	stemMap := make(map[string]string, len(srcFiles))
	for i, sf := range srcFiles {
		oldStem := StemFromPath(sf)
		newStem := StemFromPath(destFiles[i])
		if oldStem != newStem {
			stemMap[oldStem] = newStem
		}
	}

	// Scan for links that would change (without writing)
	if len(stemMap) > 0 {
		err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
				return nil
			}
			data, err := fs.ReadFile(fsys, p)
			if err != nil {
				return nil
			}
			content := string(data)
			for oldStem, newStem := range stemMap {
				_, n := RewriteWikilinks(content, oldStem, newStem)
				if n > 0 {
					result.LinkUpdates = append(result.LinkUpdates, LinkUpdate{
						File:    p,
						OldLink: oldStem,
						NewLink: newStem,
						Count:   n,
					})
				}
			}
			return nil
		})
		if err != nil {
			result.Warnings = append(result.Warnings, "dry-run link scan error: "+err.Error())
		}
	}

	// Tag mismatch on source files (check current tags against dest path)
	// Read current file data, check tags against new dest domain
	for i, sf := range srcFiles {
		data, err := fs.ReadFile(fsys, sf)
		if err != nil {
			continue
		}
		domain := domainFromPath(destFiles[i])
		if domain == "" {
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
				result.TagSuggestions = append(result.TagSuggestions, TagSuggestion{
					File:   destFiles[i],
					OldTag: tag,
					NewTag: "domain/" + domain,
				})
			}
		}
	}

	return result, nil
}

// buildStemMap creates oldStem→newStem mapping from source/dest paths.
func buildStemMap(source, dest string, movedFiles []string) map[string]string {
	stemMap := make(map[string]string)

	// For each moved file, compute old and new stems
	for _, newPath := range movedFiles {
		newStem := StemFromPath(newPath)
		// Reconstruct old path from new
		rel := strings.TrimPrefix(newPath, dest)
		rel = strings.TrimPrefix(rel, "/")
		var oldPath string
		if rel == "" {
			oldPath = source
		} else {
			oldPath = source + "/" + rel
		}
		oldStem := StemFromPath(oldPath)
		if oldStem != newStem {
			stemMap[oldStem] = newStem
		}
	}

	return stemMap
}
