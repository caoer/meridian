package watch

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/caoer/meridian/internal/hooks"
	"github.com/fsnotify/fsnotify"
)

// Watcher wraps fsnotify with debounce and ignore filtering.
type Watcher struct {
	fsw        *fsnotify.Watcher
	root       string
	ignore     []string
	debounceMs int
	events     chan []hooks.Event
	done       chan struct{}
	closeOnce  sync.Once
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
		events:     make(chan []hooks.Event, 16),
		done:       make(chan struct{}),
	}
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

// Events returns the channel of debounced event batches.
func (w *Watcher) Events() <-chan []hooks.Event {
	return w.events
}

// Close stops the watcher. Safe to call multiple times.
func (w *Watcher) Close() error {
	var err error
	w.closeOnce.Do(func() {
		close(w.done)
		err = w.fsw.Close()
	})
	w.wg.Wait()
	return err
}

func (w *Watcher) loop() {
	defer w.wg.Done()
	defer close(w.events)

	pending := make(map[string]*hooks.Event)
	var timer *time.Timer
	var timerC <-chan time.Time

	resetTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer = time.NewTimer(time.Duration(w.debounceMs) * time.Millisecond)
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
				if timer != nil {
					timer.Stop()
				}
				if len(pending) > 0 {
					w.flush(pending)
				}
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

			pending[rel] = &hooks.Event{
				Path: rel,
				Op:   op,
				Time: time.Now(),
			}
			resetTimer()

		case <-timerC:
			if len(pending) > 0 {
				w.flush(pending)
				pending = make(map[string]*hooks.Event)
			}

		case _, ok := <-w.fsw.Errors:
			if !ok {
				if timer != nil {
					timer.Stop()
				}
				if len(pending) > 0 {
					w.flush(pending)
				}
				return
			}
		}
	}
}

func (w *Watcher) flush(pending map[string]*hooks.Event) {
	batch := make([]hooks.Event, 0, len(pending))
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
