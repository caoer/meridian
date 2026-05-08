package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcher_CreateEvent(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, nil, 200)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	// Create a file
	os.WriteFile(filepath.Join(dir, "test.md"), []byte("hello"), 0644)

	select {
	case batch := <-w.Events():
		if len(batch) == 0 {
			t.Fatal("empty batch")
		}
		found := false
		for _, e := range batch {
			if filepath.Base(e.Path) == "test.md" && e.Op == "create" {
				found = true
			}
		}
		if !found {
			t.Errorf("no create event for test.md in batch: %+v", batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestWatcher_ModifyEvent(t *testing.T) {
	dir := t.TempDir()
	// Pre-create file before watcher starts
	path := filepath.Join(dir, "exist.md")
	os.WriteFile(path, []byte("v1"), 0644)

	w, err := New(dir, nil, 200)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	// Modify
	os.WriteFile(path, []byte("v2"), 0644)

	select {
	case batch := <-w.Events():
		found := false
		for _, e := range batch {
			if filepath.Base(e.Path) == "exist.md" && e.Op == "modify" {
				found = true
			}
		}
		if !found {
			t.Errorf("no modify event in batch: %+v", batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestWatcher_DeleteEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gone.md")
	os.WriteFile(path, []byte("bye"), 0644)

	w, err := New(dir, nil, 200)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	os.Remove(path)

	select {
	case batch := <-w.Events():
		found := false
		for _, e := range batch {
			if filepath.Base(e.Path) == "gone.md" && e.Op == "delete" {
				found = true
			}
		}
		if !found {
			t.Errorf("no delete event in batch: %+v", batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestWatcher_DebounceCoalesces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rapid.md")
	os.WriteFile(path, []byte("v0"), 0644)

	w, err := New(dir, nil, 500)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	// Rapid modifications within debounce window
	for i := 0; i < 5; i++ {
		os.WriteFile(path, []byte("v"+string(rune('1'+i))), 0644)
		time.Sleep(20 * time.Millisecond)
	}

	select {
	case batch := <-w.Events():
		// Should get one batch with deduplicated path
		count := 0
		for _, e := range batch {
			if filepath.Base(e.Path) == "rapid.md" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected 1 deduplicated event, got %d in batch: %+v", count, batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestWatcher_IgnoredPath(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, []string{"*.tmp"}, 200)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	// Create ignored file, then a real file
	os.WriteFile(filepath.Join(dir, "ignored.tmp"), []byte("skip"), 0644)
	time.Sleep(50 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "real.md"), []byte("keep"), 0644)

	select {
	case batch := <-w.Events():
		for _, e := range batch {
			if filepath.Base(e.Path) == "ignored.tmp" {
				t.Errorf("ignored file should not appear in events: %+v", batch)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestWatcher_SubdirWatched(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, nil, 200)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	// Create subdir and file in it
	sub := filepath.Join(dir, "subdir")
	os.MkdirAll(sub, 0755)
	time.Sleep(100 * time.Millisecond) // let watcher pick up new dir
	os.WriteFile(filepath.Join(sub, "nested.md"), []byte("hi"), 0644)

	deadline := time.After(3 * time.Second)
	for {
		select {
		case batch := <-w.Events():
			for _, e := range batch {
				if filepath.Base(e.Path) == "nested.md" {
					return // success
				}
			}
		case <-deadline:
			t.Fatal("timeout waiting for subdir event")
		}
	}
}
