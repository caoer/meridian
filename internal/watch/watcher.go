package watch

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/fsnotify/fsnotify"
)

// Event represents a filesystem change.
type Event struct {
	Path string    `json:"path"`
	Op   string    `json:"op"` // "create", "modify", "delete"
	Time time.Time `json:"time"`
}

// Watcher wraps fsnotify with debounce and ignore filtering.
type Watcher struct {
	fsw        *fsnotify.Watcher
	root       string
	ignore     []string
	debounceMs int
	events     chan []Event
	done       chan struct{}
	wg         sync.WaitGroup
}

// New creates a Watcher on root, ignoring paths matching ignore globs,
// with debounce window in milliseconds.
func New(root string, ignore []string, debounceMs int) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Walk and add all existing directories
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			if rel != "." && shouldIgnore(rel, ignore) {
				return filepath.SkipDir
			}
			fsw.Add(path)
		}
		return nil
	})

	w := &Watcher{
		fsw:        fsw,
		root:       root,
		ignore:     ignore,
		debounceMs: debounceMs,
		events:     make(chan []Event, 16),
		done:       make(chan struct{}),
	}
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

// Events returns the channel of debounced event batches.
func (w *Watcher) Events() <-chan []Event {
	return w.events
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	close(w.done)
	err := w.fsw.Close()
	w.wg.Wait()
	return err
}

func (w *Watcher) loop() {
	defer w.wg.Done()

	pending := make(map[string]*Event)
	var timer *time.Timer
	var timerC <-chan time.Time

	resetTimer := func() {
		if timer == nil {
			timer = time.NewTimer(time.Duration(w.debounceMs) * time.Millisecond)
		} else {
			timer.Reset(time.Duration(w.debounceMs) * time.Millisecond)
		}
		timerC = timer.C
	}

	for {
		select {
		case <-w.done:
			if timer != nil {
				timer.Stop()
			}
			// Flush remaining
			if len(pending) > 0 {
				w.flush(pending)
			}
			return

		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}

			rel, err := filepath.Rel(w.root, ev.Name)
			if err != nil {
				continue
			}
			if shouldIgnore(rel, w.ignore) {
				continue
			}

			// Auto-watch new directories
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					w.fsw.Add(ev.Name)
				}
			}

			op := classifyOp(ev.Op)
			if op == "" {
				continue
			}

			pending[rel] = &Event{
				Path: rel,
				Op:   op,
				Time: time.Now(),
			}
			resetTimer()

		case <-timerC:
			if len(pending) > 0 {
				w.flush(pending)
				pending = make(map[string]*Event)
			}

		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *Watcher) flush(pending map[string]*Event) {
	batch := make([]Event, 0, len(pending))
	for _, e := range pending {
		batch = append(batch, *e)
	}
	select {
	case w.events <- batch:
	case <-w.done:
	}
}

func classifyOp(op fsnotify.Op) string {
	switch {
	case op.Has(fsnotify.Create):
		return "create"
	case op.Has(fsnotify.Write):
		return "modify"
	case op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename):
		return "delete"
	default:
		return ""
	}
}

func shouldIgnore(rel string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := doublestar.Match(p, rel); matched {
			return true
		}
		// Also match against basename for simple patterns
		if matched, _ := doublestar.Match(p, filepath.Base(rel)); matched {
			return true
		}
	}
	return false
}
