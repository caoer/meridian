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

// RuleHash returns a SHA-256 hex digest that captures a rule's full identity:
// ID, check type, severity, message template, on filter, and params.
func RuleHash(r rules.Rule) string {
	h := sha256.New()
	fmt.Fprintf(h, "id:%s\n", r.ID)
	fmt.Fprintf(h, "check:%s\n", r.Check)
	fmt.Fprintf(h, "severity:%s\n", r.Severity.String())
	fmt.Fprintf(h, "message:%s\n", r.Message)

	// On filter — sorted for deterministic hashing.
	paths := make([]string, len(r.On.Paths))
	copy(paths, r.On.Paths)
	sort.Strings(paths)
	for _, p := range paths {
		fmt.Fprintf(h, "on:path:%s\n", p)
	}
	tags := make([]string, len(r.On.Tags))
	copy(tags, r.On.Tags)
	sort.Strings(tags)
	for _, tag := range tags {
		fmt.Fprintf(h, "on:tag:%s\n", tag)
	}
	excludeStrs := make([]string, len(r.On.Excludes))
	for i, e := range r.On.Excludes {
		if e.Path != "" {
			excludeStrs[i] = "path:" + e.Path
		} else {
			excludeStrs[i] = "tag:" + e.Tag
		}
	}
	sort.Strings(excludeStrs)
	for _, s := range excludeStrs {
		fmt.Fprintf(h, "on:exclude:%s\n", s)
	}

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

// ScanIdentityHash captures the configuration inputs that change which findings a
// document produces without changing the document's own bytes: the scan root, the
// skip list, and the foreign roots. Folding it into the cache key closes the hole
// (adversarial finding 3) where a config edit would otherwise serve findings
// computed under the previous configuration. Lists are sorted for determinism.
func ScanIdentityHash(scanRoot string, skip, foreignRoots []string) string {
	h := sha256.New()
	fmt.Fprintf(h, "root:%s\n", scanRoot)

	sk := make([]string, len(skip))
	copy(sk, skip)
	sort.Strings(sk)
	for _, s := range sk {
		fmt.Fprintf(h, "skip:%s\n", s)
	}

	fr := make([]string, len(foreignRoots))
	copy(fr, foreignRoots)
	sort.Strings(fr)
	for _, f := range fr {
		fmt.Fprintf(h, "foreign:%s\n", f)
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
