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
	// Key is the frontmatter key(s) a set_property wrote (comma-joined for a batch);
	// empty for body-plane writes.
	Key   string `json:"key,omitempty"`
	Rev   string `json:"rev"`
	Actor string `json:"actor"`
	// Hash is the xxhash8 of the write payload — dedupe key only, never the payload.
	Hash string `json:"hash,omitempty"`
	// Forced lists the I4 warning RULE IDS this write overrode with Force() —
	// metadata (rule vocabulary), never content. The fleet def census consumes
	// these counts and the per-agent force-rate derives from them (R-force).
	Forced []string `json:"forced,omitempty"`
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
// BY actor with op "append" within the dedupe window — the single-append dedupe
// lookup, unchanged in shape since U3.
func recentAppendHashes(target, section, actor string) map[string]bool {
	return recentHashes(target, section, "", "append", actor)
}

// recentHashes returns the set of payload hashes journaled for the exact
// (path, section, key, op) address BY actor within the dedupe window. The actor
// scoping is load-bearing: dedupe must absorb only the SAME actor's at-least-once
// retry, never a DIFFERENT actor's byte-identical (but distinct) write, which
// would vanish with no audit line (MED-4). The (section, key, op) triple is a
// batch's coalesced identity, so a retried batch matches only an identical batch.
// It reads the journal best-effort (a missing/garbled journal yields an empty
// set — dedupe is an optimization, never a correctness gate).
func recentHashes(target, section, key, op, actor string) map[string]bool {
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
		if e.Op != op || e.Section != section || e.Key != key || e.Hash == "" || e.Actor != actor {
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

// lastJournaled returns the actor and post-write file rev of the most recent
// journal entry for target — the identity of the just-completed operation on the
// file. planAnchored's omitted-rev relaxation keys its foreign_changes warning on
// it (E2 finding 3): when the journal tail is the writing actor's OWN entry and
// its rev still matches the file as re-read under the lock, every change since
// the caller's just-completed operation is provably the caller's own. Best-effort
// like every journal read: a missing or garbled journal reports ok=false — the
// caller keeps the warning (the conservative direction).
func lastJournaled(target string) (actor, rev string, ok bool) {
	f, err := os.Open(journalPath(target))
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	abs, _ := filepath.Abs(target)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var e journalEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		if !sameTarget(e.Path, target, abs) {
			continue
		}
		actor, rev, ok = e.Actor, e.Rev, true
	}
	return actor, rev, ok
}

// ForceStat aggregates one actor's journaled writes and forced-warning overrides
// — the R-force audit surface. ForcedWrites counts journal entries that carried at
// least one forced warning; ForcedWarnings counts the individual overridden rules.
type ForceStat struct {
	Writes         int `json:"writes"`
	ForcedWrites   int `json:"forced_writes"`
	ForcedWarnings int `json:"forced_warnings"`
}

// JournalForceStats reads the journal that governs target (its nearest .ccc) and
// returns per-actor force stats — the per-agent force-rate that surfaces in the
// agent's own `md def check` context (R-force). Best-effort like every journal
// read: a missing or garbled journal yields an empty map.
func JournalForceStats(target string) map[string]ForceStat {
	return forceStatsFromFile(journalPath(target), map[string]ForceStat{})
}

// ForceStatsUnder walks root for .ccc/events.ndjson journals and aggregates
// per-actor force stats fleet-wide — the def census consumer (R-force).
func ForceStatsUnder(root string) map[string]ForceStat {
	out := map[string]ForceStat{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: audit is best-effort, never fatal
		}
		if d.IsDir() && d.Name() == ".ccc" {
			forceStatsFromFile(filepath.Join(p, "events.ndjson"), out)
		}
		return nil
	})
	return out
}

// forceStatsFromFile folds one journal file's entries into acc.
func forceStatsFromFile(path string, acc map[string]ForceStat) map[string]ForceStat {
	f, err := os.Open(path)
	if err != nil {
		return acc
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var e journalEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		s := acc[e.Actor]
		s.Writes++
		if len(e.Forced) > 0 {
			s.ForcedWrites++
			s.ForcedWarnings += len(e.Forced)
		}
		acc[e.Actor] = s
	}
	return acc
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
