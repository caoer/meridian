package watch

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caoer/meridian/internal/hooks"
)

// safeBuf is a thread-safe writer for tests.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuf) snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.buf.Bytes()...)
}

func (s *safeBuf) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Len()
}

func TestDaemon_BatchProducesJSON(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, nil, 200)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	parsedHooks, _ := hooks.ParseHooks(map[string]hooks.HookDef{
		"on-create": {Action: "check", Scope: "{{.Path}}"},
	})

	buf := &safeBuf{}
	d := NewDaemon(w, parsedHooks, buf)
	go d.Run()

	os.WriteFile(filepath.Join(dir, "test.md"), []byte("hello"), 0644)
	time.Sleep(500 * time.Millisecond)

	d.Stop()

	if buf.len() == 0 {
		t.Fatal("no output produced")
	}

	var out BatchOutput
	if err := json.Unmarshal(bytes.TrimSpace(buf.snapshot()), &out); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.snapshot())
	}
	if out.Event != "batch" {
		t.Errorf("event = %q, want batch", out.Event)
	}
	if len(out.Files) == 0 {
		t.Error("no files in output")
	}
	if len(out.Results) < 1 {
		t.Errorf("results = %d, want >= 1", len(out.Results))
	}
}

func TestDaemon_StatsUpdated(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, nil, 200)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	parsedHooks, _ := hooks.ParseHooks(map[string]hooks.HookDef{
		"on-create": {Action: "check"},
	})

	buf := &safeBuf{}
	d := NewDaemon(w, parsedHooks, buf)
	go d.Run()

	os.WriteFile(filepath.Join(dir, "a.md"), []byte("a"), 0644)
	time.Sleep(500 * time.Millisecond)

	snap := d.Stats().Snapshot()
	d.Stop()

	if snap.EventsProcessed == 0 {
		t.Error("events_processed = 0")
	}
	if snap.HooksFired == 0 {
		t.Error("hooks_fired = 0")
	}
	if snap.RunningSince.IsZero() {
		t.Error("running_since is zero")
	}
}

func TestDaemon_GracefulShutdown(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, nil, 200)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	buf := &safeBuf{}
	d := NewDaemon(w, nil, buf)

	done := make(chan struct{})
	go func() {
		d.Run()
		close(done)
	}()

	d.Stop()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not shut down cleanly")
	}
}
