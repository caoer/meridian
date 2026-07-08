package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caoer/meridian/internal/hooks"
)

// writeWatchRepo creates a git-ish repo with a doc whose on-change task
// appends a line to log.txt — run count is observable as line count.
func writeWatchRepo(t *testing.T) (dir, mdPath string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `---
md-on-change: verify
md-verify: "[[doc#^verify]]"
---

` + "```bash" + `
echo ran >> log.txt
` + "```" + `

^verify
`
	mdPath = filepath.Join(dir, "doc.md")
	if err := os.WriteFile(mdPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, mdPath
}

func logLines(t *testing.T, dir string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "log.txt"))
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n")
}

// --- unit level: dispatchOnChange guards, fake runner ---

func fakeDaemon(runner OnChangeRunner) *Daemon {
	d := &Daemon{lastRunEnd: map[string]time.Time{}}
	d.onChange = runner
	return d
}

func TestDispatchOnChange_SkipsSidecarsAndNonMarkdown(t *testing.T) {
	var calls []string
	d := fakeDaemon(func(p string) *OnChangeResult {
		calls = append(calls, p)
		return &OnChangeResult{Path: p, Task: "t"}
	})
	now := time.Now()
	d.dispatchOnChange([]hooks.Event{
		{Path: "doc.runs.md", Op: "modify", Time: now},
		{Path: "script.sh", Op: "modify", Time: now},
		{Path: "doc.md", Op: "remove", Time: now},
		{Path: "doc.md", Op: "modify", Time: now},
		{Path: "doc.md", Op: "modify", Time: now}, // duplicate in batch
	})
	if len(calls) != 1 || calls[0] != "doc.md" {
		t.Fatalf("runner calls = %v, want exactly [doc.md]", calls)
	}
}

func TestDispatchOnChange_SelfCausedEventsSuppressed(t *testing.T) {
	var calls int
	d := fakeDaemon(func(p string) *OnChangeResult {
		calls++
		return &OnChangeResult{Path: p, Task: "t"}
	})
	before := time.Now()
	d.dispatchOnChange([]hooks.Event{{Path: "doc.md", Op: "modify", Time: before}})
	// Second batch: event stamped BEFORE the first run finished — the run
	// itself (or its record write) caused it.
	d.dispatchOnChange([]hooks.Event{{Path: "doc.md", Op: "modify", Time: before}})
	if calls != 1 {
		t.Fatalf("runner ran %d times, want 1 (self-caused event must not re-fire)", calls)
	}
	// A genuinely new edit (stamped after the run) fires again.
	d.dispatchOnChange([]hooks.Event{{Path: "doc.md", Op: "modify", Time: time.Now().Add(time.Second)}})
	if calls != 2 {
		t.Fatalf("runner ran %d times, want 2 (later edit must fire)", calls)
	}
}

func TestDispatchOnChange_NilRunnerNoop(t *testing.T) {
	d := &Daemon{lastRunEnd: map[string]time.Time{}}
	if got := d.dispatchOnChange([]hooks.Event{{Path: "doc.md", Op: "modify", Time: time.Now()}}); got != nil {
		t.Fatalf("want nil, got %v", got)
	}
}

// --- runner level: RunOnChange against a real repo ---

func TestRunOnChange_ExecutesAndRecords(t *testing.T) {
	dir, mdPath := writeWatchRepo(t)
	res := RunOnChange(mdPath, 30*time.Second)
	if res == nil {
		t.Fatal("declared on-change task did not run")
	}
	if res.Exit != 0 || res.Error != "" {
		t.Fatalf("result = %+v", res)
	}
	if logLines(t, dir) != 1 {
		t.Fatalf("task should have run once, log has %d lines", logLines(t, dir))
	}
	if res.Record == "" {
		t.Fatal("no run record path in result")
	}
	rec, err := os.ReadFile(res.Record)
	if err != nil {
		t.Fatalf("record not written: %v", err)
	}
	if !strings.Contains(string(rec), "block_sha: ") {
		t.Errorf("record missing block_sha:\n%s", rec)
	}
}

func TestRunOnChange_NoDeclarationReturnsNil(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	p := filepath.Join(dir, "plain.md")
	os.WriteFile(p, []byte("---\ntags: [x]\n---\n\nbody\n"), 0o644)
	if res := RunOnChange(p, time.Second); res != nil {
		t.Fatalf("want nil for doc without md-on-change, got %+v", res)
	}
}

func TestRunOnChange_UnknownTaskFailsLoudInResult(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	p := filepath.Join(dir, "bad.md")
	os.WriteFile(p, []byte("---\nmd-on-change: ghost\nmd-real: \"[[bad#^x]]\"\n---\n\n```bash\ntrue\n```\n\n^x\n"), 0o644)
	res := RunOnChange(p, time.Second)
	if res == nil || res.Error == "" || res.Exit != -1 {
		t.Fatalf("unknown task must surface as error result, got %+v", res)
	}
}

// --- end to end: real fsnotify daemon ---

func TestDaemon_OnChangeEndToEnd(t *testing.T) {
	dir, mdPath := writeWatchRepo(t)
	w, err := New(dir, nil, 150)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	buf := &safeBuf{}
	d := NewDaemon(w, nil, buf)
	d.SetOnChangeRunner(func(p string) *OnChangeResult { return RunOnChange(p, 30*time.Second) })
	go d.Run()

	// Touch the doc: append a comment so content changes.
	f, _ := os.OpenFile(mdPath, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("\nedited\n")
	f.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && logLines(t, dir) == 0 {
		time.Sleep(100 * time.Millisecond)
	}
	// Let the sidecar-write event flush through another debounce window —
	// it must NOT re-fire the task.
	time.Sleep(600 * time.Millisecond)
	d.Stop()

	if n := logLines(t, dir); n != 1 {
		t.Fatalf("task ran %d times, want exactly 1 (sidecar write must not re-trigger)", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "doc.runs.md")); err != nil {
		t.Fatal("run record not written by daemon run")
	}

	var sawOnChange bool
	for _, line := range strings.Split(strings.TrimSpace(string(buf.snapshot())), "\n") {
		var out BatchOutput
		if json.Unmarshal([]byte(line), &out) == nil && len(out.OnChange) > 0 {
			sawOnChange = true
			if out.OnChange[0].Task != "verify" || out.OnChange[0].Exit != 0 {
				t.Errorf("on_change payload wrong: %+v", out.OnChange[0])
			}
		}
	}
	if !sawOnChange {
		t.Error("no on_change entry in daemon output")
	}
	if s := d.Stats().Snapshot(); s.OnChangeRuns != 1 || s.OnChangeFailed != 0 {
		t.Errorf("stats: runs=%d failed=%d, want 1/0", s.OnChangeRuns, s.OnChangeFailed)
	}
}

func TestDaemon_OnChangeFailureCounted(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	doc := "---\nmd-on-change: boom\nmd-boom: \"[[f#^b]]\"\n---\n\n```bash\nexit 7\n```\n\n^b\n"
	p := filepath.Join(dir, "f.md")
	os.WriteFile(p, []byte(doc), 0o644)

	w, err := New(dir, nil, 150)
	if err != nil {
		t.Fatal(err)
	}
	buf := &safeBuf{}
	d := NewDaemon(w, nil, buf)
	d.SetOnChangeRunner(func(pp string) *OnChangeResult { return RunOnChange(pp, 30*time.Second) })
	go d.Run()

	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("\nedit\n")
	f.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && d.Stats().Snapshot().OnChangeRuns == 0 {
		time.Sleep(100 * time.Millisecond)
	}
	d.Stop()

	s := d.Stats().Snapshot()
	if s.OnChangeRuns != 1 || s.OnChangeFailed != 1 {
		t.Fatalf("stats: runs=%d failed=%d, want 1/1", s.OnChangeRuns, s.OnChangeFailed)
	}
	// Failure is still receipted: the record carries exit 7.
	rec, err := os.ReadFile(filepath.Join(dir, "f.runs.md"))
	if err != nil {
		t.Fatal("failed run left no record")
	}
	if !strings.Contains(string(rec), "exit: 7") {
		t.Errorf("record missing failure exit:\n%s", rec)
	}
}
