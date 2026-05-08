package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/rules"
)

// FileHash returns the SHA-256 hex digest of file content.
func FileHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// RuleHash returns a SHA-256 hex digest that captures a rule's identity:
// check type, severity, message template, and params.
func RuleHash(r rules.Rule) string {
	h := sha256.New()
	fmt.Fprintf(h, "check:%s\n", r.Check)
	fmt.Fprintf(h, "severity:%s\n", r.Severity.String())
	fmt.Fprintf(h, "message:%s\n", r.Message)

	// Deterministic param ordering.
	keys := make([]string, 0, len(r.Params))
	for k := range r.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "param:%s=%v\n", k, r.Params[k])
	}

	return hex.EncodeToString(h.Sum(nil))
}

// CombinedHash merges a file content hash with applicable rule hashes
// into a single deterministic hash. Rule hashes are sorted before combining.
func CombinedHash(fileHash string, ruleHashes []string) string {
	sorted := make([]string, len(ruleHashes))
	copy(sorted, ruleHashes)
	sort.Strings(sorted)

	h := sha256.New()
	h.Write([]byte(fileHash))
	h.Write([]byte("\n"))
	h.Write([]byte(strings.Join(sorted, "\n")))

	return hex.EncodeToString(h.Sum(nil))
}
