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
	Event    string             `json:"event"`
	Files    []hooks.Event      `json:"files"`
	Results  []hooks.HookResult `json:"results,omitempty"`
	OnChange []OnChangeResult   `json:"on_change,omitempty"`
	Time     string             `json:"time"`
}

// DaemonStats tracks daemon runtime counters.
type DaemonStats struct {
	mu              sync.Mutex
	RunningSince    time.Time `json:"running_since"`
	EventsProcessed int       `json:"events_processed"`
	LastEvent       time.Time `json:"last_event"`
	HooksFired      int       `json:"hooks_fired"`
	OnChangeRuns    int       `json:"on_change_runs"`
	OnChangeFailed  int       `json:"on_change_failed"`
}

func (s *DaemonStats) record(batchSize, hookCount, onChangeRuns, onChangeFailed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EventsProcessed += batchSize
	s.HooksFired += hookCount
	s.OnChangeRuns += onChangeRuns
	s.OnChangeFailed += onChangeFailed
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
		OnChangeRuns:    s.OnChangeRuns,
		OnChangeFailed:  s.OnChangeFailed,
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

	// md-on-change execution. lastRunEnd is touched only from the Run loop
	// goroutine (processBatch is sequential) — no lock needed.
	onChange   OnChangeRunner
	lastRunEnd map[string]time.Time
}

// SetOnChangeRunner wires md-on-change execution into the daemon. Call
// before Run.
func (d *Daemon) SetOnChangeRunner(r OnChangeRunner) {
	d.onChange = r
}

// NewDaemon creates a daemon. Call Run() to start the loop.
func NewDaemon(w *Watcher, parsedHooks []hooks.Hook, output io.Writer) *Daemon {
	d := &Daemon{
		watcher: w,
		hooks:   parsedHooks,
		stats: &DaemonStats{
			RunningSince: time.Now(),
		},
		output:     output,
		done:       make(chan struct{}),
		lastRunEnd: make(map[string]time.Time),
	}
	// Register the Run goroutine's lifetime here, before any caller can launch
	// `go d.Run()` and race Stop's wg.Wait(). Adding inside Run (the goroutine)
	// violates the WaitGroup contract — a positive Add from zero must
	// happen-before Wait — so a Stop() that beats the goroutine to its first
	// line would Wait on a zero counter and both race and skip the wait. Mirrors
	// the Watcher, which Adds before `go w.loop()`.
	d.wg.Add(1)
	return d
}

// Stats returns the daemon's stats tracker.
func (d *Daemon) Stats() *DaemonStats {
	return d.stats
}

// Run starts processing events. Blocks until Stop() or watcher closes.
// The wg lifetime is registered in NewDaemon (before any `go d.Run()`), so Run
// only signals completion here.
func (d *Daemon) Run() {
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
	onChange := d.dispatchOnChange(batch)
	failed := 0
	for _, r := range onChange {
		if r.Exit != 0 || r.Error != "" {
			failed++
		}
	}
	d.stats.record(len(batch), len(results), len(onChange), failed)

	out := BatchOutput{
		Event:    "batch",
		Files:    batch,
		Results:  results,
		OnChange: onChange,
		Time:     time.Now().Format(time.RFC3339),
	}

	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	fmt.Fprintf(d.output, "%s\n", data)
}
