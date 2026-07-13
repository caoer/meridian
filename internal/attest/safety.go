package attest

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Every attest parameter and every pin-derived frontmatter field is
// attacker-influenced the same way pin frontmatter is (§3.3 step 3, PR #12 #2):
// nothing reaches a git argv, batch stdin, or a filesystem join before it
// passes these gates. Semantics mirror internal/checks/effect_pin.go §SECURITY
// (control chars/newlines, leading '-', slug charset, ".." rejection,
// filepath.Rel containment belt); duplicated here because those helpers are
// unexported and the checks package is under parallel B2c churn.

// slugRe is the allowed charset for a repo slug or branch ref.
var slugRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// fullShaRe matches a full git object id (sha1 or sha256 hex, lowercase).
var fullShaRe = regexp.MustCompile(`^[0-9a-f]{40}$|^[0-9a-f]{64}$`)

// hasControl reports whether s contains any ASCII control char or DEL
// (includes newline/CR — the batch-stdin injection vector).
func hasControl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// checkSafeField rejects control chars/newlines and a leading '-' (option
// injection) on a field bound for git argv or batch stdin. "" when safe.
func checkSafeField(name, val string) string {
	if val == "" {
		return name + " is empty"
	}
	if hasControl(val) {
		return name + " contains control characters or newlines"
	}
	if strings.HasPrefix(val, "-") {
		return name + " has a leading '-' (git option-injection risk)"
	}
	return ""
}

// checkSlug additionally restricts a field that becomes a path segment or ref
// name: slug charset, no "..". Applies to repo (filesystem join under the
// repos root) and branch (ref name behind "origin/").
func checkSlug(name, val string) string {
	if s := checkSafeField(name, val); s != "" {
		return s
	}
	if !slugRe.MatchString(val) || strings.Contains(val, "..") {
		return fmt.Sprintf("%s %q is not a valid slug (allowed: letters, digits, . _ / -; no \"..\")", name, val)
	}
	return ""
}

// checkCommitOverride gates the {"commit": ...} param: a full lowercase-hex
// object id, nothing else — the strict-writer posture (a short or symbolic
// override would make the recorded receipt commit depend on resolution state).
func checkCommitOverride(val string) string {
	if s := checkSafeField("commit", val); s != "" {
		return s
	}
	if !fullShaRe.MatchString(val) {
		return fmt.Sprintf("commit %q must be a full lowercase-hex object id (40 or 64 chars)", val)
	}
	return ""
}

// ValidateVerdict gates the verdict param (P7): "<session-relative-path>@<full-sha>".
// The path must stay inside the session dir it is relative to — verified with a
// filepath.Rel containment belt, not string inspection — carry no control
// characters, and never be absolute or option-shaped. Returns "" when valid.
func ValidateVerdict(v string) string {
	if hasControl(v) {
		return "verdict contains control characters or newlines"
	}
	at := strings.LastIndex(v, "@")
	if at <= 0 || at == len(v)-1 {
		return fmt.Sprintf("verdict %q must have the form <path>@<sha> (P7 — the sha-qualified session verdict ref)", v)
	}
	path, sha := v[:at], v[at+1:]
	if !fullShaRe.MatchString(sha) {
		return fmt.Sprintf("verdict sha %q must be a full lowercase-hex object id (40 or 64 chars)", sha)
	}
	if strings.HasPrefix(path, "-") {
		return "verdict path has a leading '-' (option-injection risk)"
	}
	if strings.HasPrefix(path, "/") || filepath.IsAbs(filepath.FromSlash(path)) {
		return "verdict path must be session-relative, not absolute"
	}
	// Containment belt: joined under a nominal session root, the path must not
	// escape it (a ".." traversal would point the recorded ref outside the
	// append-only verdict store).
	base := string(filepath.Separator) + "session"
	joined := filepath.Join(base, filepath.FromSlash(path))
	rel, err := filepath.Rel(base, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == "." {
		return fmt.Sprintf("verdict path %q escapes the session dir", path)
	}
	return ""
}
