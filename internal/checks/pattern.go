package checks

import (
	"path/filepath"
	"regexp"
	"sync"

	"github.com/caoer/meridian/internal/engine"
)

// regexCache avoids recompiling the same pattern per-document.
// sync.Map is safe for concurrent use.
var regexCache sync.Map

func patternCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	targetRaw, _ := params["target"].(string)
	matchRaw, _ := params["match"].(string)
	if targetRaw == "" || matchRaw == "" {
		return nil
	}

	var re *regexp.Regexp
	if cached, ok := regexCache.Load(matchRaw); ok {
		re = cached.(*regexp.Regexp)
	} else {
		compiled, err := regexp.Compile(matchRaw)
		if err != nil {
			return nil
		}
		regexCache.Store(matchRaw, compiled)
		re = compiled
	}

	if targetRaw == "filename" {
		name := filepath.Base(doc.Path)
		if !re.MatchString(name) {
			return []engine.RawFinding{{
				TemplateData: map[string]string{"Filename": name},
			}}
		}
	}
	return nil
}
