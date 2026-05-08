package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caoer/meridian/internal/hooks"
	"github.com/caoer/meridian/internal/watch"
)

// safeBuf is a thread-safe bytes.Buffer for concurrent daemon output.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuf) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.buf.Bytes()...)
}

func (s *safeBuf) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Len()
}

func (s *safeBuf) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Reset()
}

func shortSock(t *testing.T, name string) string {
	t.Helper()
	path := fmt.Sprintf("/tmp/md-int-%s-%d.sock", name, os.Getpid())
	t.Cleanup(func() { os.Remove(path) })
	return path
}

func TestIntegration_DaemonFullCycle(t *testing.T) {
	dir := t.TempDir()

	w, err := watch.New(dir, []string{"*.tmp"}, 300)
	if err != nil {
		t.Fatalf("New watcher: %v", err)
	}

	parsedHooks, err := hooks.ParseHooks(map[string]hooks.HookDef{
		"on-create": {Action: "check", Scope: "{{.Path}}"},
		"on-modify": {Action: "check", Scope: "{{.Path}}"},
	})
	if err != nil {
		t.Fatalf("ParseHooks: %v", err)
	}

	buf := &safeBuf{}
	d := watch.NewDaemon(w, parsedHooks, buf)

	sockPath := shortSock(t, "cyc")
	srv, err := watch.NewStatusServer(sockPath, d.Stats())
	if err != nil {
		t.Fatalf("NewStatusServer: %v", err)
	}

	go d.Run()

	// --- Create file → hook fires ---
	os.WriteFile(filepath.Join(dir, "page.md"), []byte("---\ntags: [test]\n---\n# Page"), 0644)
	time.Sleep(600 * time.Millisecond)

	if buf.Len() == 0 {
		t.Fatal("no output after create")
	}

	var out watch.BatchOutput
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if err := json.Unmarshal(lines[0], &out); err != nil {
		t.Fatalf("invalid JSON line: %v\nraw: %s", err, lines[0])
	}
	if len(out.Results) < 1 {
		t.Errorf("results = %d after create, want >= 1", len(out.Results))
	}

	// --- Modify same file twice rapidly → single batch ---
	buf.Reset()
	os.WriteFile(filepath.Join(dir, "page.md"), []byte("---\ntags: [test]\n---\n# Page v2"), 0644)
	time.Sleep(50 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "page.md"), []byte("---\ntags: [test]\n---\n# Page v3"), 0644)
	time.Sleep(600 * time.Millisecond)

	lines = bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Errorf("expected 1 batch for rapid edits, got %d", len(lines))
	}

	// --- Ignored file → no hook ---
	buf.Reset()
	os.WriteFile(filepath.Join(dir, "scratch.tmp"), []byte("ignore me"), 0644)
	time.Sleep(100 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "real.md"), []byte("# Real"), 0644)
	time.Sleep(600 * time.Millisecond)

	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var batch watch.BatchOutput
		json.Unmarshal(line, &batch)
		for _, f := range batch.Files {
			if f.Path == "scratch.tmp" {
				t.Error("ignored file appeared in batch")
			}
		}
	}

	// --- Query status → correct counters ---
	data, err := watch.QueryStatus(sockPath)
	if err != nil {
		t.Fatalf("QueryStatus: %v", err)
	}

	var status struct {
		EventsProcessed int `json:"events_processed"`
		HooksFired      int `json:"hooks_fired"`
	}
	json.Unmarshal(data, &status)
	if status.EventsProcessed == 0 {
		t.Error("events_processed = 0 after activity")
	}
	if status.HooksFired == 0 {
		t.Error("hooks_fired = 0 after activity")
	}

	// --- Clean shutdown ---
	srv.Close()
	d.Stop()

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("socket not cleaned up after shutdown")
	}
}

func TestIntegration_SubdirEvents(t *testing.T) {
	dir := t.TempDir()

	w, err := watch.New(dir, nil, 200)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	parsedHooks, _ := hooks.ParseHooks(map[string]hooks.HookDef{
		"on-create": {Action: "check"},
	})

	buf := &safeBuf{}
	d := watch.NewDaemon(w, parsedHooks, buf)
	go d.Run()

	sub := filepath.Join(dir, "wiki")
	os.MkdirAll(sub, 0755)
	time.Sleep(150 * time.Millisecond)
	os.WriteFile(filepath.Join(sub, "nested.md"), []byte("# Nested"), 0644)
	time.Sleep(500 * time.Millisecond)

	d.Stop()

	if buf.Len() == 0 {
		t.Fatal("no output for subdir file creation")
	}
}
