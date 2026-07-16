package body

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// journal.go is the write-side audit log: after a Splice commits (tmp+fsync+rename
// succeeded), it appends ONE metadata-only line to <session>/.ccc/events.ndjson.
//
// # Metadata only — never content spans (decision 12, R-secrets)
//
// A journal entry records WHAT changed (path, section, op), by WHOM (actor), to
// WHICH revision (rev), and WHEN (ts). It never stores the content bytes that were
// written — the journal must not become a content or secret store, because /fabric
// renders fleet-wide and the file is same-uid readable. The one digest it keeps,
// Hash, is a non-reversible xxhash of the write payload used solely for the append
// dedupe window (§ dedupe); it discloses nothing reconstructable.
//
// The file is append-only, mode 0600, under a 0700 .ccc directory. A journaling
// failure NEVER fails a committed write — the bytes are already durable on disk; a
// missing audit line is logged by its absence, not by corrupting the write.

// dedupeWindow is how long an identical append is treated as a double-submit retry
// (MCP at-least-once delivery) rather than an intentional repeat. Within the
// window, a second append of byte-identical content to the same (path, section) is
// a no-op ack; after it, the same content appends again.
const dedupeWindow = 10 * time.Minute

// journalEntry is one NDJSON line. Every field is metadata; Hash is a digest, not
// content (see the file doc).
type journalEntry struct {
	TS      string `json:"ts"`
	Path    string `json:"path"`
	Section string `json:"section,omitempty"`
	Op      string `json:"op"`
	Rev     string `json:"rev"`
	Actor   string `json:"actor"`
	// Hash is the xxhash8 of the write payload — dedupe key only, never the payload.
	Hash string `json:"hash,omitempty"`
}

// journalDir resolves the .ccc directory for a target file: the nearest ancestor
// that already contains a .ccc directory, else a .ccc alongside the target. This
// keeps a session's writes journaled to that session's .ccc when the target lives
// under a session tree, and degrades to a local .ccc otherwise.
func journalDir(target string) string {
	dir, err := filepath.Abs(filepath.Dir(target))
	if err != nil {
		dir = filepath.Dir(target)
	}
	for cur := dir; ; {
		if fi, err := os.Stat(filepath.Join(cur, ".ccc")); err == nil && fi.IsDir() {
			return filepath.Join(cur, ".ccc")
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return filepath.Join(dir, ".ccc")
}

// journalPath is the events.ndjson file for a target.
func journalPath(target string) string {
	return filepath.Join(journalDir(target), "events.ndjson")
}

// appendJournal writes one metadata-only entry, best-effort. It reports whether the
// line landed so Result.Journaled reflects reality; a false return never means the
// write failed — the committed bytes are already durable.
func appendJournal(target string, e journalEntry) bool {
	dir := journalDir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	line, err := json.Marshal(e)
	if err != nil {
		return false
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.ndjson"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return false
	}
	return true
}

// recentAppendHashes returns the set of payload hashes appended to (path, section)
// with op "append" within the dedupe window, newest-relevant only. It reads the
// journal best-effort (a missing/garbled journal yields an empty set — dedupe is an
// optimization, never a correctness gate).
func recentAppendHashes(target, section string) map[string]bool {
	out := map[string]bool{}
	f, err := os.Open(journalPath(target))
	if err != nil {
		return out
	}
	defer f.Close()
	cutoff := time.Now().Add(-dedupeWindow)
	abs, _ := filepath.Abs(target)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var e journalEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		if e.Op != "append" || e.Section != section || e.Hash == "" {
			continue
		}
		if !sameTarget(e.Path, target, abs) {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, e.TS)
		if err != nil || ts.Before(cutoff) {
			continue
		}
		out[e.Hash] = true
	}
	return out
}

// sameTarget reports whether a journal entry's recorded path addresses the same
// file as target, tolerating absolute-vs-relative recording.
func sameTarget(recorded, target, absTarget string) bool {
	if recorded == target {
		return true
	}
	if absTarget != "" {
		if a, err := filepath.Abs(recorded); err == nil && a == absTarget {
			return true
		}
	}
	return false
}
