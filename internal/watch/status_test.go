package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// shortSockPath returns a short /tmp/ socket path to stay under macOS 104-byte limit.
func shortSockPath(t *testing.T, name string) string {
	t.Helper()
	path := fmt.Sprintf("/tmp/md-test-%s-%d.sock", name, os.Getpid())
	t.Cleanup(func() { os.Remove(path) })
	return path
}

func TestStatusServer_ReturnsCounters(t *testing.T) {
	stats := &DaemonStats{
		RunningSince:    time.Now(),
		EventsProcessed: 42,
		HooksFired:      18,
		LastEvent:       time.Now(),
		OnChangeRuns:    7,
		OnChangeFailed:  3,
	}

	sockPath := shortSockPath(t, "cnt")
	srv, err := NewStatusServer(sockPath, stats)
	if err != nil {
		t.Fatalf("NewStatusServer: %v", err)
	}
	defer srv.Close()

	data, err := QueryStatus(sockPath)
	if err != nil {
		t.Fatalf("QueryStatus: %v", err)
	}

	var resp struct {
		EventsProcessed int    `json:"events_processed"`
		HooksFired      int    `json:"hooks_fired"`
		RunningSince    string `json:"running_since"`
		OnChangeRuns    int    `json:"on_change_runs"`
		OnChangeFailed  int    `json:"on_change_failed"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, data)
	}
	if resp.EventsProcessed != 42 {
		t.Errorf("events_processed = %d, want 42", resp.EventsProcessed)
	}
	if resp.HooksFired != 18 {
		t.Errorf("hooks_fired = %d, want 18", resp.HooksFired)
	}
	// Serialization-boundary regression: DaemonStats counts on-change runs, but
	// handleConn's response struct once omitted them, so `md status` never
	// surfaced them despite the counters existing (watch-on-change gate finding).
	if resp.OnChangeRuns != 7 {
		t.Errorf("on_change_runs = %d, want 7", resp.OnChangeRuns)
	}
	if resp.OnChangeFailed != 3 {
		t.Errorf("on_change_failed = %d, want 3", resp.OnChangeFailed)
	}
}

func TestStatusServer_NoDaemon(t *testing.T) {
	sockPath := shortSockPath(t, "nod")
	_, err := QueryStatus(sockPath)
	if err == nil {
		t.Fatal("expected error when no daemon running")
	}
}

func TestStatusServer_MultipleQueries(t *testing.T) {
	stats := &DaemonStats{
		RunningSince: time.Now(),
	}

	sockPath := shortSockPath(t, "mul")
	srv, err := NewStatusServer(sockPath, stats)
	if err != nil {
		t.Fatalf("NewStatusServer: %v", err)
	}
	defer srv.Close()

	for i := 0; i < 3; i++ {
		_, err := QueryStatus(sockPath)
		if err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
	}
}

func TestSocketPath_Deterministic(t *testing.T) {
	a := SocketPath("/some/path/meridian.yaml")
	b := SocketPath("/some/path/meridian.yaml")
	if a != b {
		t.Errorf("same input → different paths: %q vs %q", a, b)
	}

	c := SocketPath("/other/meridian.yaml")
	if a == c {
		t.Error("different input → same path")
	}
}

func TestStatusServer_CleansUpSocket(t *testing.T) {
	sockPath := shortSockPath(t, "cln")
	srv, err := NewStatusServer(sockPath, &DaemonStats{RunningSince: time.Now()})
	if err != nil {
		t.Fatalf("NewStatusServer: %v", err)
	}
	srv.Close()

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("socket file should be removed after Close")
	}
}
