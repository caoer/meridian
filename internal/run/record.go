package run

import (
	"crypto/sha1"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caoer/meridian/internal/frontmatter"
)

// recordOutputMax bounds the per-task output retained in a run record — the
// record is a readable receipt, not a log archive. The tail is kept (failures
// end loud), and truncation is flagged in the record's frontmatter.
const recordOutputMax = 64 << 10 // 64 KiB

// cappedWriter retains the last max bytes written and remembers whether
// anything was dropped. It is shared between a task's stdout and stderr —
// os/exec services the two pipes on separate goroutines, so Write must lock.
type cappedWriter struct {
	mu        sync.Mutex
	buf       []byte
	max       int
	truncated bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, p...)
	if len(c.buf) > c.max {
		c.buf = c.buf[len(c.buf)-c.max:]
		c.truncated = true
	}
	return len(p), nil
}

func (c *cappedWriter) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.buf)
}

// BlobSHA is the git blob object id of content: sha1("blob <len>\0" + content).
// The same convention the effect pin contract uses for location checksums —
// one content-address idea across the system, reproducible with
// `git hash-object` on any clone.
func BlobSHA(content string) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write([]byte(content))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// recordEntry is one task's persisted run outcome.
type recordEntry struct {
	Block     string
	Lang      string
	Exit      int
	TimedOut  bool
	Duration  int64 // milliseconds
	At        string
	Commit    string
	BlockSHA  string
	Truncated bool
	Output    string
}

// RecordPath returns the sidecar path for a source document:
// <dir>/<stem>.runs.md.
func RecordPath(mdPath string) string {
	stem := strings.TrimSuffix(filepath.Base(mdPath), ".md")
	return filepath.Join(filepath.Dir(mdPath), stem+".runs.md")
}

// WriteRecord persists task results to the source document's sidecar run
// record and returns its path. Merge semantics: only the tasks in results are
// replaced; other tasks' records survive. The whole file is re-rendered
// deterministically (tasks sorted by name), and the source document is never
// touched — the record points back at it, it never owns it.
func WriteRecord(mdPath string, results []TaskResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("no task results to record")
	}
	recPath := RecordPath(mdPath)
	entries, err := loadRecord(recPath)
	if err != nil {
		return "", err
	}

	commit := gitHead(mdPath)
	for _, r := range results {
		entries[r.Name] = recordEntry{
			Block:     r.BlockID,
			Lang:      r.Lang,
			Exit:      r.ExitCode,
			TimedOut:  r.TimedOut,
			Duration:  r.Duration.Milliseconds(),
			At:        r.StartedAt.Format(time.RFC3339),
			Commit:    commit,
			BlockSHA:  r.BlockSHA,
			Truncated: r.OutputTruncated,
			Output:    r.Output,
		}
	}

	stem := strings.TrimSuffix(filepath.Base(mdPath), ".md")
	if err := os.WriteFile(recPath, renderRecord(stem, entries), 0o644); err != nil {
		return "", fmt.Errorf("write run record: %w", err)
	}
	return recPath, nil
}

// loadRecord parses an existing sidecar into entries; a missing file is an
// empty record. A sidecar that no longer parses is a tool failure, not a
// silent overwrite — the record may hold other tasks' history.
func loadRecord(recPath string) (map[string]recordEntry, error) {
	entries := map[string]recordEntry{}
	data, err := os.ReadFile(recPath)
	if os.IsNotExist(err) {
		return entries, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read run record: %w", err)
	}
	doc, err := frontmatter.ParseBytes(data)
	if err != nil || doc == nil {
		return nil, fmt.Errorf("existing run record %s is not parseable — refusing to overwrite it", recPath)
	}
	runs, _ := doc.Meta["runs"].(map[string]any)
	content := string(data)
	for name, raw := range runs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		e := recordEntry{
			Block:     metaString(m, "block"),
			Lang:      metaString(m, "lang"),
			Exit:      metaInt(m, "exit"),
			TimedOut:  metaBool(m, "timed_out"),
			Duration:  int64(metaInt(m, "duration_ms")),
			At:        metaString(m, "at"),
			Commit:    metaString(m, "commit"),
			BlockSHA:  metaString(m, "block_sha"),
			Truncated: metaBool(m, "truncated"),
		}
		if b, err := FindBlock(content, name); err == nil {
			e.Output = b.Code
		}
		entries[name] = e
	}
	return entries, nil
}

// renderRecord renders the full sidecar: frontmatter runs map + one
// block-addressable output section per task. Deterministic ordering so
// re-runs produce stable diffs.
func renderRecord(stem string, entries map[string]recordEntry) []byte {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("---\ntags: [type/run-record]\n")
	fmt.Fprintf(&b, "source: \"[[%s]]\"\n", stem)
	b.WriteString("runs:\n")
	for _, name := range names {
		e := entries[name]
		fmt.Fprintf(&b, "  %s:\n", name)
		fmt.Fprintf(&b, "    block: %s\n", e.Block)
		fmt.Fprintf(&b, "    lang: %s\n", e.Lang)
		fmt.Fprintf(&b, "    exit: %d\n", e.Exit)
		fmt.Fprintf(&b, "    timed_out: %t\n", e.TimedOut)
		fmt.Fprintf(&b, "    duration_ms: %d\n", e.Duration)
		fmt.Fprintf(&b, "    at: %q\n", e.At)
		fmt.Fprintf(&b, "    commit: %q\n", e.Commit)
		fmt.Fprintf(&b, "    block_sha: %s\n", e.BlockSHA)
		fmt.Fprintf(&b, "    truncated: %t\n", e.Truncated)
	}
	b.WriteString("---\n")
	for _, name := range names {
		e := entries[name]
		fence := outputFence(e.Output)
		fmt.Fprintf(&b, "\n# %s\n\n%s\n%s", name, fence, e.Output)
		if e.Output != "" && !strings.HasSuffix(e.Output, "\n") {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s\n\n^%s\n", fence, name)
	}
	return []byte(b.String())
}

// outputFence returns a backtick fence delimiter strictly longer than any
// backtick run in the output, so captured fences stay content (CommonMark
// closing-fence rule: same char, >= opener length).
func outputFence(output string) string {
	longest := 0
	run := 0
	for _, r := range output {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := longest + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}

// gitHead returns the short HEAD hash of the file's repo, best-effort — a
// record in a repo with no commits (or no git binary) carries an empty commit
// rather than failing the run it documents.
func gitHead(mdPath string) string {
	top, err := GitToplevel(mdPath)
	if err != nil {
		return ""
	}
	out, err := exec.Command("git", "-C", top, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func metaString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func metaInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func metaBool(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}
