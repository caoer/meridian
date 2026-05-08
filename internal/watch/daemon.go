package watch

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/caoer/meridian/internal/hooks"
)

// BatchOutput is one JSON line emitted per debounced batch.
type BatchOutput struct {
	Event   string         `json:"event"`
	Files   []hooks.Event  `json:"files"`
	Results []hooks.HookResult `json:"results,omitempty"`
	Time    string         `json:"time"`
}

// DaemonStats tracks daemon runtime counters.
type DaemonStats struct {
	mu              sync.Mutex
	RunningSince    time.Time `json:"running_since"`
	EventsProcessed int      `json:"events_processed"`
	LastEvent       time.Time `json:"last_event"`
	HooksFired      int      `json:"hooks_fired"`
}

func (s *DaemonStats) record(batchSize, hookCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EventsProcessed += batchSize
	s.HooksFired += hookCount
	s.LastEvent = time.Now()
}

// Snapshot returns a copy of stats safe for reading.
func (s *DaemonStats) Snapshot() DaemonStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return DaemonStats{
		RunningSince:    s.RunningSince,
		EventsProcessed: s.EventsProcessed,
		LastEvent:       s.LastEvent,
		HooksFired:      s.HooksFired,
	}
}

// Daemon runs the watch loop: watcher → dispatch → JSON output.
type Daemon struct {
	watcher  *Watcher
	hooks    []hooks.Hook
	stats    *DaemonStats
	output   io.Writer
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewDaemon creates a daemon. Call Run() to start the loop.
func NewDaemon(w *Watcher, parsedHooks []hooks.Hook, output io.Writer) *Daemon {
	return &Daemon{
		watcher: w,
		hooks:   parsedHooks,
		stats: &DaemonStats{
			RunningSince: time.Now(),
		},
		output: output,
		done:   make(chan struct{}),
	}
}

// Stats returns the daemon's stats tracker.
func (d *Daemon) Stats() *DaemonStats {
	return d.stats
}

// Run starts processing events. Blocks until Stop() or watcher closes.
func (d *Daemon) Run() {
	d.wg.Add(1)
	defer d.wg.Done()

	for {
		select {
		case <-d.done:
			return
		case batch, ok := <-d.watcher.Events():
			if !ok {
				return
			}
			d.processBatch(batch)
		}
	}
}

// Stop signals the daemon to shut down and waits for completion.
// Safe to call multiple times.
func (d *Daemon) Stop() {
	d.stopOnce.Do(func() {
		close(d.done)
		d.watcher.Close()
	})
	d.wg.Wait()
}

func (d *Daemon) processBatch(batch []hooks.Event) {
	results := hooks.Dispatch(batch, d.hooks)
	d.stats.record(len(batch), len(results))

	out := BatchOutput{
		Event:   "batch",
		Files:   batch,
		Results: results,
		Time:    time.Now().Format(time.RFC3339),
	}

	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	fmt.Fprintf(d.output, "%s\n", data)
}
